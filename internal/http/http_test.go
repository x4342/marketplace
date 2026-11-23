package web

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/x4342/marketplace/internal/seller"
)

type testHeartbeat struct {
	heartbeat pgtype.Timestamptz
	err       error
}

func (h testHeartbeat) WorkerHeartbeat(context.Context) (pgtype.Timestamptz, error) {
	return h.heartbeat, h.err
}

func testHandler(heartbeat testHeartbeat) http.Handler {
	assets := fstest.MapFS{
		"app.css":     &fstest.MapFile{Data: []byte("body{}")},
		"htmx.min.js": &fstest.MapFile{Data: []byte("// htmx")},
	}
	return routes(heartbeat, slog.New(slog.NewTextHandler(io.Discard, nil)), assets, nil, seller.Service{})
}

func TestDarkShellAndRetiredThemeRoute(t *testing.T) {
	handler := testHandler(testHeartbeat{err: pgx.ErrNoRows})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "marketplace_theme", Value: "light"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	body := response.Body.String()
	if response.Code != http.StatusOK || !strings.Contains(body, `data-theme="dark"`) ||
		!strings.Contains(body, `name="color-scheme" content="dark"`) ||
		!strings.Contains(body, "Overview") || strings.Contains(body, "/preferences/theme") {
		t.Fatal("expected dark Overview without theme controls")
	}
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(method, "/preferences/theme", nil))
		if response.Code < 400 || response.Header().Get("Set-Cookie") != "" {
			t.Fatalf("%s retired theme route returned %d or set a cookie", method, response.Code)
		}
	}
}

func TestOnionFormsPreserveSameOriginChecks(t *testing.T) {
	handler := testHandler(testHeartbeat{})
	origin := "http://" + strings.Repeat("a", 56) + ".onion"
	for _, test := range []struct {
		requestOrigin string
		want          int
	}{
		{origin, http.StatusMethodNotAllowed},
		{"null", http.StatusForbidden},
		{"http://other.test", http.StatusForbidden},
	} {
		request := httptest.NewRequest(http.MethodPost, origin+"/health/live", nil)
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Origin", test.requestOrigin)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != test.want || response.Header().Get("Referrer-Policy") != "same-origin" {
			t.Fatalf("origin %q: status %d, referrer policy %q", test.requestOrigin, response.Code, response.Header().Get("Referrer-Policy"))
		}
	}

	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if policy := response.Header().Get("Referrer-Policy"); policy != "no-referrer" {
		t.Fatalf("loopback policy: %q", policy)
	}
}

func TestDatabaseStatusAndLiveness(t *testing.T) {
	for _, tt := range []struct {
		name      string
		source    testHeartbeat
		readiness int
		status    string
	}{
		{"unavailable", testHeartbeat{err: errors.New("private driver detail")}, http.StatusServiceUnavailable, "Status unavailable"},
		{"migrated without worker", testHeartbeat{err: pgx.ErrNoRows}, http.StatusOK, "No recent heartbeat"},
		{"stale worker", testHeartbeat{heartbeat: pgtype.Timestamptz{Time: time.Now().Add(-time.Minute), Valid: true}}, http.StatusOK, "No recent heartbeat"},
		{"active worker", testHeartbeat{heartbeat: pgtype.Timestamptz{Time: time.Now(), Valid: true}}, http.StatusOK, "Active"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			handler := testHandler(tt.source)
			for path, want := range map[string]int{"/health/ready": tt.readiness, "/health/live": http.StatusOK, "/fragments/status": http.StatusOK} {
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
				if response.Code != want || strings.Contains(response.Body.String(), "private driver detail") {
					t.Fatalf("%s returned %d, body %q", path, response.Code, response.Body.String())
				}
				if path == "/fragments/status" && (!strings.Contains(response.Body.String(), tt.status) ||
					strings.Contains(response.Body.String(), "<html")) {
					t.Fatalf("unexpected fragment: %s", response.Body.String())
				}
			}
		})
	}
}

func TestAssetsAndNotFound(t *testing.T) {
	handler := testHandler(testHeartbeat{})
	for path, want := range map[string]int{"/assets/app.css": http.StatusOK, "/assets/htmx.min.js": http.StatusOK, "/assets/": http.StatusNotFound, "/unknown": http.StatusNotFound} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != want || response.Header().Get("Content-Security-Policy") == "" {
			t.Fatalf("%s returned %d with headers %v", path, response.Code, response.Header())
		}
	}
}
