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

func TestAdminPostgres(t *testing.T) {
	dsn := os.Getenv("MARKETPLACE_ADMIN_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("use a migrated, seeded disposable database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("cannot open test database")
	}
	defer pool.Close()
	q := db.New(pool)
	accounts := identity.New(pool)
	checkout := orders.Simulation(pool)
	handler, err := New(q, accounts, seller.Service{Pool: pool}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	register := func(prefix string) (*testClient, pgtype.UUID) {
		t.Helper()
		var token [6]byte
		_, _ = rand.Read(token[:])
		client := &testClient{t: t, handler: handler}
		client.call("GET", "/register", nil, 200)
		client.post("/register", url.Values{"username": {prefix + "_" + hex.EncodeToString(token[:])}, "password": {"isolated moderation fixture password"}}, 200)
		actor, err := accounts.Active(ctx, client.session)
		if err != nil {
			t.Fatal(err)
		}
		return client, actor.ID
	}
	buyer, buyerID := register("buyer")
	merchant, merchantID := register("seller")
	moderator, modID := register("mod")
	second, secondID := register("appealmod")
	finance, financeID := register("finance")
	outsider, _ := register("other")
	if _, err := pool.Exec(ctx, "INSERT INTO role_assignments(user_id,role) VALUES ($1,'moderator'),($2,'moderator'),($3,'moderator')", modID, secondID, financeID); err != nil {
		t.Fatal(err)
	}
	var listing pgtype.UUID
	err = pool.QueryRow(ctx, `INSERT INTO listings(seller_id,category_slug,title,description,kind,price_minor,currency,stock,condition,ship_regions,delivery_mode,terms,status,published_at)
	VALUES($1,'accessories','Administration fixture','Synthetic item','physical',2000,'EUR',10,'new',ARRAY['EU'],'','Test-only fulfillment','published',now()) RETURNING id`, merchantID).Scan(&listing)
	if err != nil {
		t.Fatal(err)
	}
	product := "/listings/" + listing.String()
	thread := buyer.post(product+"/conversation", url.Values{}, 303).Header().Get("Location")
	buyer.post(thread+"/send", url.Values{"message": {"Private conversation payload"}}, 303)
	buyer.post(thread+"/report", url.Values{"reason": {"Synthetic chat report"}}, 303)
	var chatID pgtype.UUID
	if err := pool.QueryRow(ctx, "SELECT id FROM content_reports WHERE conversation_id IS NOT NULL AND reporter_id=$1", buyerID).Scan(&chatID); err != nil {
		t.Fatal(err)
	}
	chat := "/admin/content/" + chatID.String()
	body := moderator.call("GET", chat, nil, 200).Body.String()
	if strings.Contains(body, "Private conversation payload") || !strings.Contains(body, "Synthetic chat report") {
		t.Fatal("moderator report body scope incorrect")
	}
	moderator.call("GET", thread, nil, 404)
	decision := func(client *testClient, path, action string, want int) {
		t.Helper()
		client.post(path, url.Values{"action": {action}, "reason": {"Synthetic administrative decision"}, "staff_password": {"isolated moderation fixture password"}}, want)
	}
	decision(moderator, chat+"/decision", "restrict", 303)
	merchant.post(thread+"/send", url.Values{"message": {"Blocked content"}}, 403)
	buyer.post("/account/moderation/"+chatID.String()+"/appeal", url.Values{"reason": {"Synthetic chat appeal"}}, 303)
	decision(moderator, chat+"/decision", "restore", 409)
	decision(second, chat+"/decision", "restore", 303)
	merchant.post(thread+"/send", url.Values{"message": {"Restored conversation"}}, 303)

	buyer.post(product+"/report", url.Values{"kind": {"listing"}, "reason": {"Synthetic listing report"}}, 303)
	buyer.post(product+"/report", url.Values{"kind": {"seller"}, "reason": {"Synthetic seller report"}}, 303)
	var listingCase, userCase pgtype.UUID
	if err := pool.QueryRow(ctx, "SELECT id FROM content_reports WHERE listing_id=$1", listing).Scan(&listingCase); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT id FROM content_reports WHERE subject_user_id=$1", merchantID).Scan(&userCase); err != nil {
		t.Fatal(err)
	}
	outsider.call("GET", "/admin/content", nil, 403)
	listingDecision := "/admin/content/" + listingCase.String() + "/decision"
	decision(moderator, listingDecision, "restrict", 303)
	buyer.call("GET", product, nil, 404)
	merchant.post("/sell/listings/"+listing.String()+"/status", url.Values{"status": {"published"}}, 404)
	merchant.post("/account/moderation/"+listingCase.String()+"/appeal", url.Values{"reason": {"Synthetic listing appeal"}}, 303)
	decision(second, listingDecision, "restore", 303)
	merchant.post("/sell/listings/"+listing.String()+"/status", url.Values{"status": {"published"}}, 303)
	buyer.call("GET", product, nil, 200)

	purchase := func() pgtype.UUID {
		t.Helper()
		id, err := checkout.Create(ctx, orders.Checkout{Buyer: buyerID, Listing: listing, Key: identity.NewToken(), Region: "EU", Address: "Private fictional shipping marker"})
		if err != nil {
			t.Fatal(err)
		}
		if err := checkout.SimulatePayment(ctx, id, buyerID); err != nil {
			t.Fatal(err)
		}
		return id
	}
	paid := purchase()
	userDecision := "/admin/content/" + userCase.String() + "/decision"
	decision(moderator, userDecision, "restrict", 303)
	merchant.post(thread+"/send", url.Values{"message": {"Restricted sender"}}, 403)
	if err := checkout.Act(ctx, paid, merchantID, "dispatch", "TEST-013"); err != nil {
		t.Fatalf("restriction blocked existing fulfillment: %v", err)
	}

	buyer.post(orderPath(paid)+"/actions", url.Values{"action": {"problem"}, "category": {"not_received"}, "details": {"Synthetic non-delivery dispute"}}, 303)
	dispute, err := q.OrderDispute(ctx, paid)
	if err != nil {
		t.Fatal(err)
	}
	private := "/disputes/" + dispute.String()
	admin := "/admin" + private
	outsider.call("GET", private, nil, 404)
	outsider.call("GET", admin, nil, 403)
	finance.call("GET", orderPath(paid), nil, 404)
	outsider.post(private+"/evidence", url.Values{"kind": {"evidence"}, "body": {"Injected evidence"}}, 404)
	merchant.post(private+"/evidence", url.Values{"kind": {"evidence"}, "body": {"Synthetic dispatch evidence"}}, 303)
	evidence := moderator.call("GET", admin, nil, 200).Body.String()
	if strings.Contains(evidence, "Private fictional shipping marker") || !strings.Contains(evidence, "Synthetic dispatch evidence") || !strings.Contains(evidence, "Dispatch recorded.") || strings.Contains(evidence, "TEST-013") {
		t.Fatal("dispute evidence not scoped or shipping spilled into case")
	}
	decision(outsider, admin+"/decision", "release", 403)
	decision(outsider, "/admin/payment-adjustments/"+paid.String(), "refund", 403)
	decision(finance, "/admin/payment-adjustments/"+paid.String(), "refund", 409)

	balances := func(available, held, reserved int64) {
		t.Helper()
		rows, err := q.SellerBalances(ctx, merchantID)
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]int64{}
		for _, row := range rows {
			got[row.Bucket] = row.Balance
		}
		if got["available"] != available || got["held"] != held || got["reserved"] != reserved {
			t.Fatalf("unexpected case balances %v", got)
		}
	}
	balances(0, 1900, 0)
	decision(moderator, admin+"/decision", "hold", 303)
	balances(0, 1900, 0)
	decision(moderator, admin+"/decision", "release", 303)
	decision(moderator, admin+"/decision", "release", 409)
	balances(1900, 0, 0)
	buyer.post(private+"/appeal", url.Values{"reason": {"Synthetic order appeal"}}, 303)
	appeals, err := q.DisputeAppeals(ctx, dispute)
	if err != nil || len(appeals) != 1 {
		t.Fatalf("appeal persistence: %v", err)
	}
	appeal := url.Values{"action": {"referred"}, "reason": {"Independent review requested"}, "appeal": {appeals[0].ID.String()}}
	moderator.post(admin+"/decision", appeal, 403)
	second.post(admin+"/decision", appeal, 303)
	balances(1900, 0, 0)
	if !strings.Contains(buyer.call("GET", "/notifications", nil, 200).Body.String(), private) {
		t.Fatal("dispute notification did not deep-link")
	}

	refund := purchase()
	buyer.post(orderPath(refund)+"/actions", url.Values{"action": {"problem"}, "category": {"delivery_failed"}, "details": {"Synthetic refund dispute"}}, 303)
	refundCase, err := q.OrderDispute(ctx, refund)
	if err != nil {
		t.Fatal(err)
	}
	decision(moderator, "/admin/disputes/"+refundCase.String()+"/decision", "refund", 303)
	balances(1900, 0, 0)

	key := identity.NewToken()
	r := merchant.post("/payouts", url.Values{"key": {key}, "amount": {"5.00"}, "currency": {"EUR"}, "scenario": {"success"}}, 303)
	payoutPath := "/admin" + strings.TrimSuffix(r.Header().Get("Location"), "?created=1") + "/action"
	decision(outsider, payoutPath, "review", 403)
	decision(finance, payoutPath, "review", 303)
	decision(finance, payoutPath, "hold", 303)
	decision(finance, payoutPath, "approve", 409)
	decision(finance, payoutPath, "deny", 303)
	balances(1400, 500, 0)
	decision(finance, payoutPath, "lift_hold", 303)
	balances(1900, 0, 0)

	staffAudit := moderator.call("GET", "/admin/audit", nil, 200).Body.String()
	if !strings.Contains(staffAudit, "dispute · view") || !strings.Contains(staffAudit, "payouts · denied") {
		t.Fatal("staff audit did not include both authorized areas")
	}
	outsider.call("GET", "/admin/audit", nil, 403)
	if _, err := pool.Exec(ctx, "UPDATE admin_audit_events SET reason='rewrite'"); err == nil {
		t.Fatal("audit history mutable")
	}
}
