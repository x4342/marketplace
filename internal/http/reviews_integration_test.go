package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/x4342/marketplace/internal/db"
	"github.com/x4342/marketplace/internal/identity"
	"github.com/x4342/marketplace/internal/orders"
	"github.com/x4342/marketplace/internal/seller"
)

func TestReviewsPostgres(t *testing.T) {
	dsn := os.Getenv("MARKETPLACE_REVIEWS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("use a migrated, seeded disposable database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("cannot open test database")
	}
	defer pool.Close()
	accounts := identity.New(pool)
	service := orders.Simulation(pool)
	handler, err := New(db.New(pool), accounts, seller.Service{Pool: pool}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	buyer := testClient{t: t, handler: handler}
	merchant := testClient{t: t, handler: handler}
	outsider := testClient{t: t, handler: handler}
	moderator := testClient{t: t, handler: handler}
	finance := testClient{t: t, handler: handler}
	ids := make(map[*testClient]db.ActiveSessionRow)
	for client, prefix := range map[*testClient]string{&buyer: "buyer", &merchant: "merchant", &outsider: "outsider", &moderator: "mod", &finance: "finance"} {
		var token [6]byte
		_, _ = rand.Read(token[:])
		client.call("GET", "/register", nil, 200)
		client.post("/register", url.Values{"username": {prefix + "_" + hex.EncodeToString(token[:])}, "password": {"isolated safe fixture password"}}, 200)
		account, err := accounts.Active(ctx, client.session)
		if err != nil {
			t.Fatal(err)
		}
		ids[client] = account
	}
	if _, err := pool.Exec(ctx, "INSERT INTO role_assignments(user_id,role) VALUES ($1,'moderator'),($2,'finance_admin')", ids[&moderator].ID, ids[&finance].ID); err != nil {
		t.Fatal(err)
	}
	var listing pgtype.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO listings (seller_id,category_slug,title,description,kind,price_minor,currency,stock,delivery_mode,terms,status,published_at)
		VALUES ($1,'templates','Verified review fixture','Review fixture description','digital',1250,'EUR',10,'manual','Private fulfillment terms','published',now())
		RETURNING id`, ids[&merchant].ID).Scan(&listing); err != nil {
		t.Fatal(err)
	}
	public := "/listings/" + listing.String()
	buy := func() pgtype.UUID {
		t.Helper()
		id, err := service.Create(ctx, orders.Checkout{Buyer: ids[&buyer].ID, Listing: listing, Key: identity.NewToken()})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	pay := func(id pgtype.UUID) {
		t.Helper()
		if err := service.SimulatePayment(ctx, id, ids[&buyer].ID); err != nil {
			t.Fatal(err)
		}
	}
	complete := func(id pgtype.UUID) {
		t.Helper()
		if err := service.Act(ctx, id, ids[&merchant].ID, "deliver", "Private delivery marker"); err != nil {
			t.Fatal(err)
		}
		if err := service.Act(ctx, id, ids[&buyer].ID, "complete", ""); err != nil {
			t.Fatal(err)
		}
	}
	form := func(rating, body string) url.Values { return url.Values{"stars": {rating}, "body": {body}} }
	id := buy()
	path := orderPath(id) + "/reviews"
	buyer.call("POST", path, form("5", "missing token"), 403)
	buyer.post(path, form("5", "unpaid"), 409)
	if _, err := pool.Exec(ctx, `INSERT INTO reviews(order_id,author_id,listing_id,seller_id,stars,body)
		VALUES ($1,$2,$3,$4,5,'unpaid direct write')`, id, ids[&buyer].ID, listing, ids[&merchant].ID); err == nil {
		t.Fatal("database accepted an unverified review")
	}
	merchant.post(path, form("5", "seller self-review"), 404)
	outsider.post(path, form("5", "unrelated buyer"), 404)
	pay(id)
	buyer.post(path, form("5", "paid but not completed"), 409)
	complete(id)
	buyer.post(path, form("6", "invalid rating"), 422)
	buyer.post(path, form("4", " "), 422)
	buyer.post(path, form("5", "Verified <strong>fixture</strong>"), 303)
	buyer.post(path, form("5", "duplicate"), 409)
	var rid pgtype.UUID
	if err := pool.QueryRow(ctx, "SELECT id FROM reviews WHERE order_id=$1", id).Scan(&rid); err != nil {
		t.Fatal(err)
	}
	private := "/reviews/" + rid.String()
	body := outsider.call("GET", public, nil, 200).Body.String()
	for _, want := range []string{"Verified purchase", "Verified &lt;strong&gt;fixture&lt;/strong&gt;", "average shown after 3", "Item rating distribution", "Reviews for this item: 1"} {
		if !strings.Contains(body, want) {
			t.Fatalf("public page missing %q", want)
		}
	}
	for _, sensitive := range []string{id.String(), ids[&buyer].Username, "Private delivery marker"} {
		if strings.Contains(body, sensitive) {
			t.Fatalf("public review exposes %q", sensitive)
		}
	}
	sellerFilter := "&seller=" + ids[&merchant].Username
	if strings.Contains(outsider.call("GET", "/discover?rating=4"+sellerFilter, nil, 200).Body.String(), ">Verified review fixture<") {
		t.Fatal("minimum sample is not enforced for rating filter")
	}
	if !strings.Contains(outsider.call("GET", "/discover?reviewed=1"+sellerFilter, nil, 200).Body.String(), ">Verified review fixture<") {
		t.Fatal("review presence filter missed the listing")
	}

	if body := outsider.call("GET", "/discover?sort=popular", nil, 200).Body.String(); strings.Contains(body, `value="popular"`) {
		t.Fatal("popularity was offered without enough purchases")
	}
	for _, stars := range []string{"4", "5"} {
		next := buy()
		pay(next)
		complete(next)
		buyer.post(orderPath(next)+"/reviews", form(stars, "Additional verified purchase"), 303)
	}
	if body := outsider.call("GET", "/discover?rating=4&sort=rating"+sellerFilter, nil, 200).Body.String(); !strings.Contains(body, "4.7 out of 5") {
		t.Fatal("rating filter or real seller summary unavailable after minimum sample")
	}
	if body := outsider.call("GET", "/discover?sort=popular"+sellerFilter, nil, 200).Body.String(); !strings.Contains(body, `value="popular" selected`) {
		t.Fatal("popularity did not unlock after verified completions")
	}
	store := outsider.call("GET", "/stores/"+ids[&merchant].Username, nil, 200).Body.String()
	if !strings.Contains(store, "4.7 out of 5") || !strings.Contains(store, "Member since") {
		t.Fatal("storefront lacks trust metadata")
	}
	if !strings.Contains(merchant.call("GET", "/notifications", nil, 200).Body.String(), private) {
		t.Fatal("seller notification did not deep link")
	}
	outsider.call("GET", private, nil, 404)
	buyer.post(private+"/report", url.Values{"reason": {"self-report"}}, 409)
	merchant.post(private+"/report", url.Values{"reason": {"Fixture report reason"}}, 303)
	merchant.post(private+"/report", url.Values{"reason": {"duplicate report"}}, 409)
	finance.call("GET", "/admin/reviews", nil, 403)
	decision := "/admin/reviews/" + rid.String() + "/decision"
	finance.post(decision, url.Values{"action": {"hide"}, "reason": {"invalid role"}}, 403)
	if !strings.Contains(moderator.call("GET", "/admin/reviews", nil, 200).Body.String(), "Fixture report reason") {
		t.Fatal("moderator queue lost report")
	}
	moderator.post(decision, url.Values{"action": {"hide"}, "reason": {"Fixture hide reason"}}, 303)
	if strings.Contains(outsider.call("GET", public, nil, 200).Body.String(), "Verified &lt;strong&gt;fixture") {
		t.Fatal("hidden review stayed public")
	}
	if strings.Contains(outsider.call("GET", "/discover?rating=4"+sellerFilter, nil, 200).Body.String(), ">Verified review fixture<") {
		t.Fatal("hidden review counted toward public threshold")
	}
	if !strings.Contains(buyer.call("GET", private, nil, 200).Body.String(), "Fixture hide reason") {
		t.Fatal("review author could not see the moderation reason")
	}
	outsider.post(private+"/appeal", url.Values{"reason": {"unrelated"}}, 404)
	buyer.post(private+"/appeal", url.Values{"reason": {"Fixture appeal reason"}}, 303)
	buyer.post(private+"/appeal", url.Values{"reason": {"duplicate appeal"}}, 409)
	moderator.post(decision, url.Values{"action": {"restore"}, "reason": {"Fixture restore reason"}}, 303)
	if !strings.Contains(outsider.call("GET", public, nil, 200).Body.String(), "Verified &lt;strong&gt;fixture") {
		t.Fatal("restored review did not return")
	}
	if _, err := pool.Exec(ctx, "UPDATE reviews SET body='changed' WHERE id=$1", rid); err == nil {
		t.Fatal("immutable review body changed")
	}
	if _, err := pool.Exec(ctx, "UPDATE review_moderation_actions SET reason='changed' WHERE review_id=$1", rid); err == nil {
		t.Fatal("moderation history changed")
	}
	if _, err := pool.Exec(ctx, "UPDATE orders SET state='refunded' WHERE id=$1", id); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(outsider.call("GET", public, nil, 200).Body.String(), "Order later refunded") {
		t.Fatal("refund annotation absent")
	}
	if !strings.Contains(merchant.call("GET", "/sell/insights", nil, 200).Body.String(), "EUR 25.00") {
		t.Fatal("seller gross totals are incorrect")
	}
	if strings.Contains(outsider.call("GET", "/sell/insights?owner="+ids[&merchant].ID.String(), nil, 200).Body.String(), "EUR 25.00") {
		t.Fatal("seller totals exposed to another account")
	}
}
