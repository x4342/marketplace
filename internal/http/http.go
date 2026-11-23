package web

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/a-h/templ"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/x4342/marketplace/internal/db"
	"github.com/x4342/marketplace/internal/finance"
	"github.com/x4342/marketplace/internal/identity"
	"github.com/x4342/marketplace/internal/moderation"
	"github.com/x4342/marketplace/internal/orders"
	"github.com/x4342/marketplace/internal/reviews"
	"github.com/x4342/marketplace/internal/seller"
	"github.com/x4342/marketplace/web/static"
	"github.com/x4342/marketplace/web/templates"
)

var onionHost = regexp.MustCompile(`(?i)^[a-z2-7]{56}\.onion(?::[0-9]{1,5})?$`)

type heartbeatReader interface {
	WorkerHeartbeat(context.Context) (pgtype.Timestamptz, error)
}

type app struct {
	db  heartbeatReader
	log *slog.Logger
}

func New(queries *db.Queries, accounts *identity.Service, sales seller.Service, log *slog.Logger) (http.Handler, error) {
	assets, err := static.Assets()
	if err != nil {
		return nil, err
	}
	return routes(queries, log, assets, accounts, sales), nil
}

func routes(heartbeat heartbeatReader, log *slog.Logger, assets fs.FS, accounts *identity.Service, sales seller.Service) http.Handler {
	app := app{db: heartbeat, log: log}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", app.home)
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /health/ready", app.ready)
	mux.HandleFunc("GET /fragments/status", app.statusFragment)
	if accounts != nil {
		app.mountIdentity(mux, accounts)
	}
	if queries, ok := heartbeat.(*db.Queries); ok {
		app.mountCatalog(mux, queries, accounts)
		pages := identityPages{app: app, accounts: accounts}
		profilePages{identityPages: pages, queries: queries}.mount(mux)
		sellerPages{identityPages: pages, queries: queries, service: sales}.mount(mux)
		messagePages{identityPages: pages, queries: queries, pool: sales.Pool}.mount(mux)
		checkout := orders.Simulation(sales.Pool)
		adminPages{identityPages: pages, queries: queries, content: moderation.Service{Pool: sales.Pool}, checkout: checkout}.mount(mux)
		orderPages{identityPages: pages, queries: queries, service: checkout}.mount(mux)
		financePages{identityPages: pages, queries: queries, ledger: finance.Simulation(sales.Pool), checkout: checkout}.mount(mux)
		reviewPages{identityPages: pages, queries: queries, service: reviews.Service{Pool: sales.Pool}}.mount(mux)
	}

	assetHandler := http.StripPrefix("/assets/", http.FileServerFS(assets))
	for _, path := range []string{"/assets/app.css", "/assets/htmx.min.js", "/assets/seller-upload.js"} {
		mux.Handle("GET "+path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "public, max-age=3600")
			assetHandler.ServeHTTP(w, r)
		}))
	}

	csrf := http.NewCrossOriginProtection()
	csrf.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		app.render(w, r, http.StatusForbidden, templates.Problem("Request denied", "Submit the form from the marketplace page."))
	}))
	protected := csrf.Handler(mux)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		referrerPolicy := "no-referrer"
		if onionHost.MatchString(r.Host) {
			// Preserve Origin on same-origin form POSTs from HTTP onion services.
			referrerPolicy = "same-origin"
		}
		w.Header().Set("Referrer-Policy", referrerPolicy)
		protected.ServeHTTP(w, r)
	})
}

func (a app) home(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		a.render(w, r, http.StatusNotFound, templates.Problem("Page unavailable", "The page you requested could not be found."))
		return
	}
	a.render(w, r, http.StatusOK, templates.Overview(a.status(r.Context())))
}

func (a app) statusFragment(w http.ResponseWriter, r *http.Request) {
	a.render(w, r, http.StatusOK, templates.SystemStatus(a.status(r.Context())))
}

func (a app) ready(w http.ResponseWriter, r *http.Request) {
	if !a.status(r.Context()).DatabaseReady {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	_, _ = w.Write([]byte("ready\n"))
}

func (a app) status(ctx context.Context) templates.Status {
	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	heartbeat, err := a.db.WorkerHeartbeat(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return templates.Status{DatabaseReady: true}
	}
	if err != nil {
		return templates.Status{}
	}
	return templates.Status{
		DatabaseReady: true,
		WorkerActive:  heartbeat.Valid && time.Since(heartbeat.Time) <= 45*time.Second,
	}
}

func (a app) render(w http.ResponseWriter, r *http.Request, status int, component templ.Component) {
	var buffer bytes.Buffer
	if err := component.Render(r.Context(), &buffer); err != nil {
		a.log.Error("template rendering failed")
		http.Error(w, "Unable to display this page. Please retry.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buffer.WriteTo(w)
}
