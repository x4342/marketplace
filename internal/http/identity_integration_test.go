package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/x4342/marketplace/internal/db"
	"github.com/x4342/marketplace/internal/identity"
	"github.com/x4342/marketplace/internal/seller"
)

func TestIdentityPostgres(t *testing.T) {
	dsn := os.Getenv("MARKETPLACE_IDENTITY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MARKETPLACE_IDENTITY_TEST_DATABASE_URL to a migrated test database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("cannot open test database")
	}
	defer pool.Close()

	service := identity.New(pool)
	handler, err := New(db.New(pool), service, seller.Service{Pool: pool}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}

	var suffix [7]byte
	_, _ = rand.Read(suffix[:])
	aliceName, bobName := "alice"+hex.EncodeToString(suffix[:]), "bob"+hex.EncodeToString(suffix[:])
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM security_events WHERE user_id IN (SELECT id FROM users WHERE username IN ($1, $2))", aliceName, bobName)
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE username IN ($1, $2)", aliceName, bobName)
	}()

	alice := testClient{t: t, handler: handler}
	bob := testClient{t: t, handler: handler}
	password := "fixture password 123"
	nextPassword := "replacement password 456"

	alice.call("POST", "/register", url.Values{"username": {aliceName}, "password": {password}}, 403)
	alice.post("/register", url.Values{"username": {"with_under"}, "password": {"12345678"}}, 422)
	alice.post("/register", url.Values{"username": {"badspace"}, "password": {"1234567"}}, 422)
	alice.post("/register", url.Values{"username": {"oversized"}, "password": {strings.Repeat("x", 4100)}}, 403)
	emoji := strings.Repeat("🦊", 126) + "é "
	emojiName := "emoji" + aliceName[5:]
	emojiUser := testClient{t: t, handler: handler}
	emojiUser.call("GET", "/register", nil, 200)
	emojiUser.post("/register", url.Values{"username": {strings.ToUpper(emojiName)}, "password": {emoji}}, 200)
	emojiUser.post("/login", url.Values{"username": {emojiName}, "password": {strings.TrimSpace(emoji)}}, 422)
	emojiUser.post("/login", url.Values{"username": {strings.ToUpper(emojiName)}, "password": {emoji}}, 303)

	alice.call("GET", "/register", nil, 200)
	enrollment := alice.post("/register", url.Values{"username": {aliceName}, "password": {password}}, 200)
	codePattern := regexp.MustCompile(`<code>([a-zA-Z0-9_-]{43})</code>`)
	codes := codePattern.FindAllStringSubmatch(enrollment.Body.String(), -1)
	if len(codes) != 5 || enrollment.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("expected five private recovery codes, got %d", len(codes))
	}
	oldCookie := alice.session
	if !strings.Contains(alice.call("GET", "/account", nil, 200).Body.String(), aliceName) {
		t.Fatal("account does not show its own username")
	}
	alice.call("GET", "/admin/moderation", nil, 403)
	alice.call("GET", "/admin/finance", nil, 403)

	bob.call("GET", "/register", nil, 200)
	bob.post("/register", url.Values{"username": {strings.ToUpper(aliceName)}, "password": {password}}, 409)
	bob.post("/register", url.Values{"username": {bobName}, "password": {password}}, 200)

	a, err := service.Active(ctx, alice.session)
	if err != nil {
		t.Fatal(err)
	}
	b, err := service.Active(ctx, bob.session)
	if err != nil {
		t.Fatal(err)
	}
	if identity.Owns(a.ID, b.ID) || !identity.Owns(a.ID, a.ID) {
		t.Fatal("ownership guard returned an incorrect result")
	}
	if err := service.Logout(ctx, b.ID, alice.session); err != identity.ErrInvalid {
		t.Fatalf("cross-account session revocation returned %v", err)
	}
	alice.call("GET", "/account", nil, 200)

	if _, err := pool.Exec(ctx, "INSERT INTO role_assignments VALUES ($1, 'moderator')", a.ID); err != nil {
		t.Fatal(err)
	}
	alice.call("GET", "/admin/moderation", nil, 200)
	alice.call("GET", "/admin/finance", nil, 200)
	alice.call("GET", "/ops/health", nil, 403)
	bob.call("GET", "/admin/finance", nil, 403)
	bob.call("GET", "/admin/moderation", nil, 403)
	bob.call("GET", "/ops/health", nil, 403)

	alice.post("/recover", url.Values{"username": {aliceName}, "password": {nextPassword}, "code": {codes[0][1]}}, 200)
	if _, err := service.Active(ctx, oldCookie); err != identity.ErrInvalid {
		t.Fatalf("previous session survived recovery: %v", err)
	}
	alice.post("/recover", url.Values{"username": {aliceName}, "password": {nextPassword}, "code": {codes[0][1]}}, 422)
	alice.post("/login", url.Values{"username": {aliceName}, "password": {password}}, 422)
	recoverySession := alice.session
	alice.post("/login", url.Values{"username": {aliceName}, "password": {nextPassword}}, 303)
	if _, err := service.Active(ctx, recoverySession); err != identity.ErrInvalid {
		t.Fatalf("pre-rotation session remained active: %v", err)
	}
	current := alice.session
	alice.call("POST", "/logout", url.Values{}, 403)
	alice.post("/logout", url.Values{}, 303)
	if _, err := service.Active(ctx, current); err != identity.ErrInvalid {
		t.Fatalf("logged-out session remained active: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE sessions SET created_at = now() - interval '2 days', expires_at = now() - interval '1 day' WHERE user_id = $1", b.ID); err != nil {
		t.Fatal(err)
	}
	bob.call("GET", "/account", nil, 303)
	var events int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM security_events WHERE user_id = $1", a.ID).Scan(&events); err != nil || events != 6 {
		t.Fatalf("expected six Alice security events, got %d (%v)", events, err)
	}
}

type testClient struct {
	t             *testing.T
	handler       http.Handler
	csrf, session string
}

func (c *testClient) post(path string, form url.Values, want int) *httptest.ResponseRecorder {
	c.t.Helper()
	form.Set("csrf", c.csrf)
	return c.call(http.MethodPost, path, form, want)
}

func (c *testClient) call(method, path string, form url.Values, want int) *httptest.ResponseRecorder {
	c.t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	if method == http.MethodPost {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for name, value := range map[string]string{csrfCookie: c.csrf, sessionCookie: c.session} {
		if value != "" {
			request.AddCookie(&http.Cookie{Name: name, Value: value})
		}
	}
	response := httptest.NewRecorder()
	c.handler.ServeHTTP(response, request)
	if response.Code != want {
		c.t.Fatalf("%s %s: got %d, want %d", method, path, response.Code, want)
	}
	for _, cookie := range response.Result().Cookies() {
		switch cookie.Name {
		case csrfCookie:
			c.csrf = cookie.Value
		case sessionCookie:
			c.session = cookie.Value
		}
	}
	return response
}
