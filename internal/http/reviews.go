package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/x4342/marketplace/internal/db"
	"github.com/x4342/marketplace/internal/reviews"
	"github.com/x4342/marketplace/web/templates"
)

type reviewPages struct {
	identityPages
	queries *db.Queries
	service reviews.Service
}

func (p reviewPages) mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /orders/{id}/reviews", p.create)
	mux.HandleFunc("GET /reviews/{id}", p.detail)
	mux.HandleFunc("POST /reviews/{id}/report", p.report)
	mux.HandleFunc("POST /reviews/{id}/appeal", p.appeal)
	mux.HandleFunc("GET /admin/reviews", p.queue)
	mux.HandleFunc("POST /admin/reviews/{id}/decision", p.decide)
	mux.HandleFunc("GET /sell/insights", p.insights)
}

func (p reviewPages) create(w http.ResponseWriter, r *http.Request) {
	session, ok := p.mutation(w, r)
	if !ok {
		return
	}
	stars, _ := strconv.ParseInt(r.PostForm.Get("stars"), 10, 16)
	id := listingID(r)
	if err := p.service.Create(r.Context(), session.ID, id, int16(stars), r.PostForm.Get("body")); err != nil {
		p.failure(w, r, err)
		return
	}
	http.Redirect(w, r, orderPath(id)+"?review=saved#review", http.StatusSeeOther)
}

func (p reviewPages) detail(w http.ResponseWriter, r *http.Request) {
	session, ok := p.requireSession(w, r)
	if !ok {
		return
	}
	row, err := p.queries.PrivateReview(r.Context(), db.PrivateReviewParams{ReviewID: listingID(r), Actor: session.ID})
	if err != nil {
		p.failure(w, r, err)
		return
	}
	p.render(w, r, http.StatusOK, templates.PrivateReview(theme(r), row, row.AuthorID == session.ID, csrfFor(w, r), r.URL.Query().Get("appeal") == "sent"))
}

func (p reviewPages) report(w http.ResponseWriter, r *http.Request) {
	session, ok := p.mutation(w, r)
	if !ok {
		return
	}
	id := listingID(r)
	listing, err := p.service.Report(r.Context(), session.ID, id, r.PostForm.Get("reason"))
	if err != nil {
		p.failure(w, r, err)
		return
	}
	http.Redirect(w, r, "/listings/"+listing.String()+"?report=sent#reviews", http.StatusSeeOther)
}

func (p reviewPages) appeal(w http.ResponseWriter, r *http.Request) {
	session, ok := p.mutation(w, r)
	if !ok {
		return
	}
	id := listingID(r)
	if err := p.service.Appeal(r.Context(), session.ID, id, r.PostForm.Get("reason")); err != nil {
		p.failure(w, r, err)
		return
	}
	http.Redirect(w, r, "/reviews/"+id.String()+"?appeal=sent", http.StatusSeeOther)
}

func (p reviewPages) queue(w http.ResponseWriter, r *http.Request) {
	if _, ok := p.moderator(w, r); !ok {
		return
	}
	page := inboxPage(r)
	rows, err := p.queries.PendingReviewCases(r.Context(), 30*(page-1))
	if err != nil {
		p.failure(w, r, err)
		return
	}
	more := len(rows) > 30
	if more {
		rows = rows[:30]
	}
	cases := make([]templates.ReviewCase, 0, len(rows))
	for _, row := range rows {
		reasons, err := p.queries.ReviewCaseReasons(r.Context(), row.ID)
		if err != nil {
			p.failure(w, r, err)
			return
		}
		cases = append(cases, templates.ReviewCase{Review: row, Reasons: reasons})
	}
	p.render(w, r, http.StatusOK, templates.ReviewQueue(theme(r), cases, csrfFor(w, r), page, more, r.URL.Query().Get("updated") == "1"))
}

func (p reviewPages) decide(w http.ResponseWriter, r *http.Request) {
	if !validForm(w, r) {
		p.denied(w, r)
		return
	}
	session, ok := p.moderator(w, r)
	if !ok {
		return
	}
	if err := p.service.Decide(r.Context(), session.ID, listingID(r), r.PostForm.Get("action"), r.PostForm.Get("reason")); err != nil {
		p.failure(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/reviews?updated=1", http.StatusSeeOther)
}

func (p reviewPages) insights(w http.ResponseWriter, r *http.Request) {
	session, ok := p.requireSession(w, r)
	if !ok {
		return
	}
	totals, err := p.queries.SellerActivity(r.Context(), session.ID)
	if err != nil {
		p.failure(w, r, err)
		return
	}
	gross, err := p.queries.SellerGrossSales(r.Context(), session.ID)
	if err != nil {
		p.failure(w, r, err)
		return
	}
	p.render(w, r, http.StatusOK, templates.SellerInsights(theme(r), totals, gross))
}

func (p reviewPages) moderator(w http.ResponseWriter, r *http.Request) (db.ActiveSessionRow, bool) {
	session, ok := p.requireSession(w, r)
	if ok && !session.Moderator {
		p.render(w, r, http.StatusForbidden, templates.Problem(theme(r), "Access denied", "Review moderation requires the content moderator role."))
		return session, false
	}
	return session, ok
}

func (p reviewPages) mutation(w http.ResponseWriter, r *http.Request) (db.ActiveSessionRow, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	if err := r.ParseForm(); err != nil || !validCSRF(r) {
		p.denied(w, r)
		return db.ActiveSessionRow{}, false
	}
	return p.requireSession(w, r)
}

func (p reviewPages) failure(w http.ResponseWriter, r *http.Request, err error) {
	status, title, detail := http.StatusServiceUnavailable, "Reviews unavailable", "Try again shortly."
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		status, title, detail = http.StatusNotFound, "Review resource unavailable", "Open your order or the original listing."
	case errors.Is(err, reviews.ErrConflict):
		status, title, detail = http.StatusConflict, "Review action unavailable", "Check the order or review’s current state."
	case errors.Is(err, reviews.ErrInvalid):
		status, title, detail = http.StatusUnprocessableEntity, "Invalid review fields", "Choose between one and five stars and enter text within the stated limit."
	default:
		p.log.Error("review operation failed")
	}
	if strings.HasPrefix(r.URL.Path, "/orders/") {
		p.render(w, r, status, templates.OrderProblem(theme(r), title, detail))
		return
	}
	p.render(w, r, status, templates.Problem(theme(r), title, detail))
}
