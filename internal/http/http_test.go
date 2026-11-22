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

func TestThemePOSTAndRender(t *testing.T) {
	handler := testHandler(testHeartbeat{err: pgx.ErrNoRows})
	request := httptest.NewRequest(http.MethodPost, "/preferences/theme", strings.NewReader("theme=dark"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/" {
		t.Fatalf("theme submission returned %d, location %q", response.Code, response.Header().Get("Location"))
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d theme cookies, want 1", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != themeCookie || cookie.Value != "dark" || !cookie.HttpOnly ||
		cookie.SameSite != http.SameSiteStrictMode || cookie.MaxAge <= 0 {
		t.Fatalf("unexpected theme cookie: %v", cookie)
	}

	request = httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(cookie)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `data-theme="dark"`) ||
		!strings.Contains(response.Body.String(), "No recent heartbeat") {
		t.Fatalf("theme or database status missing: %d %s", response.Code, response.Body.String())
	}
}

func TestThemeRejectsInvalidAndCrossSitePosts(t *testing.T) {
	handler := testHandler(testHeartbeat{})
	for _, tt := range []struct {
		name, body, fetchSite string
		want                  int
	}{
		{"unknown", "theme=blue", "", http.StatusBadRequest},
		{"duplicate", "theme=dark&theme=light", "", http.StatusBadRequest},
		{"too large", "theme=" + strings.Repeat("a", 1100), "", http.StatusBadRequest},
		{"cross site", "theme=dark", "cross-site", http.StatusForbidden},
	} {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/preferences/theme", strings.NewReader(tt.body))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if tt.fetchSite != "" {
				request.Header.Set("Sec-Fetch-Site", tt.fetchSite)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tt.want || response.Header().Get("Set-Cookie") != "" {
				t.Fatalf("got %d and cookie %q, want %d and no cookie", response.Code, response.Header().Get("Set-Cookie"), tt.want)
			}
		})
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
