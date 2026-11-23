package web

import (
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/x4342/marketplace/internal/db"
	"github.com/x4342/marketplace/internal/identity"
	"github.com/x4342/marketplace/internal/seller"
)

func TestPlatformPostgres(t *testing.T) {
	dsn := os.Getenv("MARKETPLACE_PLATFORM_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires an isolated, migrated and seeded database")
	}
	ctx := t.Context()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	svc := identity.New(pool)
	handler, err := New(db.New(pool), svc, seller.Service{Pool: pool}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	guest := testClient{t: t, handler: handler}
	admin := testClient{t: t, handler: handler}
	member := testClient{t: t, handler: handler}
	memberName := "testm13member"
	password := identity.NewToken()
	memberPassword := identity.NewToken()
	account, err := svc.Register(ctx, memberName, memberPassword)
	if err != nil {
		t.Fatal(err)
	}
	member.session = account.Session
	if _, err := svc.BootstrapAdmin(ctx, password, "", false); err != nil {
		t.Fatal(err)
	}
	admin.session, err = svc.Login(ctx, "admin", password, "")
	if err != nil {
		t.Fatal(err)
	}
	private := []string{"/admin", "/admin/users", "/admin/categories", "/admin/categories/unknown", "/admin/status", "/fragments/status", "/ops/health"}
	for _, path := range private {
		guest.call("GET", path, nil, 303)
		member.call("GET", path, nil, 403)
	}
	guest.call("GET", "/", nil, 303)
	member.call("GET", "/staff", nil, 403)
	member.call("GET", "/admin/payouts", nil, 403)
	member.call("GET", "/admin/content", nil, 403)
	admin.call("GET", "/admin/users", nil, 200)
	admin.call("GET", "/admin/categories", nil, 200)
	admin.call("GET", "/admin/status", nil, 200)
	changeRole := func(operator *testClient, target, previous, next, credential string, result int) {
		t.Helper()
		operator.post("/admin/users/role", url.Values{"username": {target}, "previous": {previous}, "role": {next}, "confirm": {"1"}, "staff_password": {credential}}, result)
	}
	changeRole(&admin, "admin", "admin", "user", password, 409)
	changeRole(&admin, memberName, "user", "admin", identity.NewToken(), 403)
	changeRole(&admin, memberName, "admin", "user", password, 409)
	changeRole(&admin, memberName, "user", "moderator", password, 303)
	member.call("GET", "/staff", nil, 303)
	member.session, err = svc.Login(ctx, memberName, memberPassword, "")
	if err != nil {
		t.Fatal(err)
	}
	member.call("GET", "/staff", nil, 200)
	member.call("GET", "/admin/payouts", nil, 200)
	member.call("GET", "/admin/content", nil, 200)
	member.call("GET", "/admin/categories", nil, 403)
	member.call("GET", "/admin/users", nil, 403)
	member.post("/admin/categories", url.Values{}, 403)
	changeRole(&member, memberName, "moderator", "admin", memberPassword, 403)
	changeRole(&admin, memberName, "moderator", "admin", password, 303)
	member.session, err = svc.Login(ctx, memberName, memberPassword, "")
	if err != nil {
		t.Fatal(err)
	}
	member.call("GET", "/admin", nil, 200)
	member.call("GET", "/admin/users", nil, 200)
	changeRole(&member, "admin", "admin", "user", memberPassword, 303)
	admin.call("GET", "/admin", nil, 303)
	changeRole(&member, memberName, "admin", "moderator", memberPassword, 409)

	cat := func(path, name, slug, parent, rev, active string, expected int) {
		t.Helper()
		member.post(path, url.Values{"name": {name}, "slug": {slug}, "parent": {parent}, "revision": {rev}, "display_order": {"12"}, "active": {active}, "confirm": {"1"}, "description": {"Synthetic category details."}}, expected)
	}
	const categories = "/admin/categories"
	var root, child, baselineID string
	loadID := func(slug string, dest *string) {
		t.Helper()
		if err := pool.QueryRow(ctx, "SELECT id::text FROM categories WHERE slug=$1", slug).Scan(dest); err != nil {
			t.Fatal(err)
		}
	}
	loadID("accessories", &baselineID)
	cat(categories, "Office equipment", "", "", "0", "1", 303)
	loadID("office-equipment", &root)
	cat(categories, "Audio accessories", "audio-accessories", root, "0", "1", 303)
	loadID("audio-accessories", &child)
	cat(categories, "Duplicate", "office-equipment", "", "0", "1", 409)
	cat(categories, "Bad Slug", "Bad Slug", "", "0", "1", 422)
	cat(categories+"/"+root, "Circular parent", "office-equipment", root, "1", "1", 422)
	cat(categories+"/"+root, "Nested parent", "office-equipment", child, "1", "1", 422)
	cat(categories, "Too deep", "too-deep", child, "0", "1", 422)
	member.call("POST", categories, url.Values{"name": {"missing csrf"}}, 403)

	cat(categories+"/"+baselineID, "Keyboard accessories", "keyboard-accessories", root, "1", "1", 303)
	cat(categories+"/"+baselineID, "Stale edit", "stale", root, "1", "1", 409)
	cat(categories, "Reserved alias", "accessories", "", "0", "1", 409)
	var newID string
	loadID("keyboard-accessories", &newID)
	if newID != baselineID {
		t.Fatal("category identity changed when the slug changed")
	}
	for _, path := range []string{"/discover?category=accessories", "/discover?category=office-equipment", "/stores/workshop?category=keyboard-accessories"} {
		if got := guest.call("GET", path, nil, 200).Body.String(); !strings.Contains(got, "Compact mechanical keyboard") {
			t.Fatalf("filter %s lost the existing listing", path)
		}
	}
	cat(categories+"/"+root, "Office equipment", "office-equipment", "", "1", "", 303)
	form := member.call("GET", "/sell/new", nil, 200).Body.String()
	if strings.Contains(form, `value="keyboard-accessories"`) || strings.Contains(form, `value="audio-accessories"`) {
		t.Fatal("inactive category tree is selectable by a new listing")
	}
	draft := member.post("/sell/new", url.Values{
		"title": {"Inactive category fixture"}, "description": {"Synthetic listing draft"},
		"category": {"accessories"}, "kind": {"physical"}, "price": {"12.50"},
		"currency": {"EUR"}, "stock": {"2"}, "condition": {"used"},
		"region_eu": {"1"}, "terms": {"Synthetic terms"},
	}, 303).Header().Get("Location")
	draft = strings.Split(draft, "?")[0]
	if preview := member.call("GET", draft+"/preview", nil, 200).Body.String(); !strings.Contains(preview, "Choose a current category.") {
		t.Fatal("saved draft was allowed to publish into an inactive category")
	}
	redirect := member.post(draft+"/publish", url.Values{"revision": {"1"}}, 303).Header().Get("Location")
	if redirect != draft+"/preview" {
		t.Fatal("publication accepted an inactive legacy slug")
	}
	detail := guest.call("GET", "/listings/892351c3-15c9-468e-a61f-b948658c18e5", nil, 200).Body.String()
	if !strings.Contains(detail, "Keyboard accessories") {
		t.Fatal("inactive category lost existing listing details")
	}
	panel := member.call("GET", "/admin", nil, 200).Body.String()
	if !strings.Contains(panel, "updated keyboard-accessories") || !strings.Contains(panel, "set admin from admin to user") {
		t.Fatal("administrator activity is missing from dashboard")
	}
	if _, err := pool.Exec(ctx, "UPDATE category_change_events SET after_state='{}'"); err == nil {
		t.Fatal("category history can be rewritten")
	}
	if _, err := pool.Exec(ctx, "UPDATE categories SET parent_slug='audio-accessories' WHERE slug='office-equipment'"); err == nil {
		t.Fatal("database accepted a category cycle")
	}
}
