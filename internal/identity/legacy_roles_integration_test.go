package identity

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/x4342/marketplace/db/migrations"
	"github.com/x4342/marketplace/internal/authz"
	"github.com/x4342/marketplace/internal/db"
)

func TestLegacyRolesPostgres(t *testing.T) {
	dsn := os.Getenv("MARKETPLACE_LEGACY_ROLE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires an empty disposable database")
	}
	ctx := context.Background()
	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	provider, err := goose.NewProvider(goose.DialectPostgres, conn, migrations.Files)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 11); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	q := db.New(pool)
	password := NewToken()
	hash := passwordHash(password)
	for _, name := range []string{"admin", "legacy_finance", "legacy_content", "legacy_both"} {
		if _, err := q.CreateUser(ctx, db.CreateUserParams{Username: name, PasswordHash: hash}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO role_assignments (user_id, role)
		SELECT u.id, a.role FROM users u JOIN (VALUES
			('legacy_finance', 'finance_admin'), ('legacy_content', 'moderator'),
			('legacy_both', 'moderator'), ('legacy_both', 'finance_admin')
		) AS a(username, role) ON a.username=u.username`); err != nil {
		t.Fatal(err)
	}
	admin, err := q.UserCredential(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	original, err := enroll(ctx, q, admin.ID, "register")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{
		"admin": authz.User, "legacy_finance": authz.User,
		"legacy_content": authz.User, "legacy_both": authz.Moderator,
	} {
		u, err := q.UserCredential(ctx, name)
		if err != nil {
			t.Fatal(err)
		}
		got, err := q.EffectiveRole(ctx, u.ID)
		if err != nil || got != want {
			t.Fatalf("%s: effective role %q, expected %q (%v)", name, got, want, err)
		}
	}
	var archived int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM legacy_role_assignments").Scan(&archived); err != nil || archived != 4 {
		t.Fatalf("legacy grants not preserved: %d (%v)", archived, err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM legacy_role_assignments"); err == nil {
		t.Fatal("legacy grants were mutable")
	}
	svc := New(pool)
	if _, err := svc.BootstrapAdmin(ctx, password, "", false); !errors.Is(err, ErrAdminNameInUse) {
		t.Fatalf("implicit legacy claim: %v", err)
	}
	if _, err := svc.BootstrapAdmin(ctx, NewToken(), original.Codes[0], true); !errors.Is(err, ErrInvalid) {
		t.Fatalf("wrong claim password: %v", err)
	}
	if _, err := svc.BootstrapAdmin(ctx, password, NewToken(), true); !errors.Is(err, ErrInvalid) {
		t.Fatalf("wrong recovery code: %v", err)
	}
	codes, err := svc.BootstrapAdmin(ctx, password, original.Codes[0], true)
	if err != nil || len(codes) != len(original.Codes) {
		t.Fatalf("verified claim failed: %v", err)
	}
	after, err := q.UserCredential(ctx, "admin")
	if err != nil || after.ID != admin.ID || !bytes.Equal(after.PasswordHash, admin.PasswordHash) {
		t.Fatal("verified claim changed user identity or password")
	}
	if _, err := svc.Active(ctx, original.Session); !errors.Is(err, ErrInvalid) {
		t.Fatalf("pre-claim session survived: %v", err)
	}
	if _, err := svc.Recover(ctx, "admin", original.Codes[1], NewToken()); !errors.Is(err, ErrInvalid) {
		t.Fatalf("pre-claim recovery code survived: %v", err)
	}
	if _, err := svc.Recover(ctx, "admin", codes[0], NewToken()); err != nil {
		t.Fatalf("replacement recovery code was rejected: %v", err)
	}
	if _, err := provider.Down(ctx); err == nil {
		t.Fatal("rollback allowed with populated role history")
	}
}
