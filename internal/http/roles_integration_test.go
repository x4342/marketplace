package web

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/x4342/marketplace/internal/authz"
	"github.com/x4342/marketplace/internal/db"
	"github.com/x4342/marketplace/internal/identity"
	"github.com/x4342/marketplace/internal/seller"
)

func TestRolesPostgres(t *testing.T) {
	dsn := os.Getenv("MARKETPLACE_ROLES_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires a disposable migrated database without an administrator")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	accounts := identity.New(pool)
	handler, err := New(db.New(pool), accounts, seller.Service{Pool: pool}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	admin := testClient{t: t, handler: handler}
	ordinary := testClient{t: t, handler: handler}
	password := identity.NewToken()
	admin.call("GET", "/register", nil, 200)
	admin.post("/register", url.Values{"username": {" AdMiN "}, "password": {password}}, 422)
	if _, err := accounts.BootstrapAdmin(ctx, password, identity.NewToken(), true); !errors.Is(err, identity.ErrInvalid) {
		t.Fatalf("claim accepted with no existing account: %v", err)
	}
	codes, err := accounts.BootstrapAdmin(ctx, password, "", false)
	if err != nil || len(codes) != 5 {
		t.Fatalf("bootstrap failed: %v", err)
	}
	if again, err := accounts.BootstrapAdmin(ctx, identity.NewToken(), "", false); !errors.Is(err, identity.ErrBootstrapComplete) || len(again) != 0 {
		t.Fatal("bootstrap was not one-time")
	}
	admin.call("GET", "/login", nil, 200)
	admin.post("/login", url.Values{"username": {"admin"}, "password": {password}}, 303)
	admin.call("GET", "/ops/health", nil, 200)
	for _, path := range []string{"/admin/payouts", "/admin/content", "/admin/audit"} {
		admin.call("GET", path, nil, 200)
	}
	name := "role_" + strings.NewReplacer("-", "a", "_", "b").Replace(strings.ToLower(identity.NewToken()[:12]))
	enrolled, err := accounts.Register(ctx, name, password)
	if err != nil {
		t.Fatal(err)
	}
	ordinary.session = enrolled.Session
	active, err := accounts.Active(ctx, ordinary.session)
	if err != nil || active.Role != authz.User {
		t.Fatalf("unexpected default role %q", active.Role)
	}
	ordinary.call("GET", "/ops/health", nil, 403)
	ordinary.call("GET", "/admin/payouts", nil, 403)
	ordinary.call("GET", "/admin/content", nil, 403)
	if err := accounts.OperatorSetRole(ctx, identity.NewToken(), name, authz.Moderator); !errors.Is(err, identity.ErrOperator) {
		t.Fatal("invalid operator credential accepted")
	}
	if err := accounts.OperatorSetRole(ctx, password, name, authz.Admin); !errors.Is(err, identity.ErrOperator) {
		t.Fatal("additional admin could be assigned")
	}
	if err := accounts.OperatorSetRole(ctx, password, name, authz.Moderator); err != nil {
		t.Fatal(err)
	}
	if _, err := accounts.Active(ctx, ordinary.session); !errors.Is(err, identity.ErrInvalid) {
		t.Fatal("existing sessions survived role change")
	}
	ordinary.call("GET", "/login", nil, 200)
	ordinary.post("/login", url.Values{"username": {name}, "password": {password}}, 303)
	ordinary.call("GET", "/admin/payouts", nil, 200)
	ordinary.call("GET", "/admin/content", nil, 200)
	ordinary.call("GET", "/ops/health", nil, 403)
	ordinary.post("/admin/payouts/00000000-0000-0000-0000-000000000000/action", url.Values{"action": {"review"}, "reason": {"Missing fresh password"}}, 403)
	ordinary.post("/admin/payouts/00000000-0000-0000-0000-000000000000/action", url.Values{"action": {"review"}, "reason": {"Incorrect password"}, "staff_password": {identity.NewToken()}}, 403)
	next := identity.NewToken()
	admin.post("/recover", url.Values{"username": {"admin"}, "password": {next}, "code": {codes[0]}}, 200)
	admin.call("GET", "/ops/health", nil, 200)
	if err := accounts.RestoreAdmin(ctx, password); !errors.Is(err, identity.ErrInvalid) {
		t.Fatal("old password could restore role")
	}
	if err := accounts.RestoreAdmin(ctx, next); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE role_change_events SET new_role='admin'"); err == nil {
		t.Fatal("role history is mutable")
	}
}
