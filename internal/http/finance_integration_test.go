package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/x4342/marketplace/internal/db"
	"github.com/x4342/marketplace/internal/finance"
	"github.com/x4342/marketplace/internal/identity"
	"github.com/x4342/marketplace/internal/orders"
	"github.com/x4342/marketplace/internal/seller"
)

func TestFinancePostgres(t *testing.T) {
	dsn := os.Getenv("MARKETPLACE_FINANCE_TEST_DATABASE_URL")
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
	ledger := finance.Simulation(pool)
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
		client.post("/register", url.Values{"username": {prefix + "_" + hex.EncodeToString(token[:])}, "password": {"isolated finance fixture password"}}, 200)
		actor, err := accounts.Active(ctx, client.session)
		if err != nil {
			t.Fatal(err)
		}
		return client, actor.ID
	}
	sellerClient, sellerID := register("seller")
	_, buyerID := register("buyer")
	admin, adminID := register("finance")
	moderator, modID := register("mod")
	if _, err := pool.Exec(ctx, "INSERT INTO role_assignments(user_id,role) VALUES ($1,'finance_admin'),($2,'moderator')", adminID, modID); err != nil {
		t.Fatal(err)
	}

	var listing pgtype.UUID
	err = pool.QueryRow(ctx, `INSERT INTO listings(seller_id,category_slug,title,description,kind,price_minor,currency,stock,delivery_mode,terms,status,published_at)
	VALUES($1,'templates','Finance transaction fixture','Fixture detail','digital',2000,'EUR',10,'manual','Test-only delivery','published',now()) RETURNING id`, sellerID).Scan(&listing)
	if err != nil {
		t.Fatal(err)
	}

	balances := func(pending, available, held, reserved, withdrawn int64) {
		t.Helper()
		rows, err := q.SellerBalances(ctx, sellerID)
		if err != nil {
			t.Fatal(err)
		}
		values := map[string]int64{}
		for _, row := range rows {
			if row.Denomination != "EUR" {
				t.Fatal("mixed account denomination")
			}
			values[row.Bucket] = row.Balance
		}
		if values["pending"] != pending || values["available"] != available || values["held"] != held ||
			values["reserved"] != reserved || values["withdrawn"] != withdrawn {
			t.Fatalf("unexpected balances: %v", values)
		}
	}
	purchase := func() pgtype.UUID {
		t.Helper()
		id, err := checkout.Create(ctx, orders.Checkout{Buyer: buyerID, Listing: listing, Key: identity.NewToken()})
		if err != nil {
			t.Fatal(err)
		}
		for range 2 {
			if err := checkout.SimulatePayment(ctx, id, buyerID); err != nil {
				t.Fatal(err)
			}
		}
		return id
	}
	complete := func(id pgtype.UUID) {
		t.Helper()
		if err := checkout.Act(ctx, id, sellerID, "deliver", "Test delivery note"); err != nil {
			t.Fatal(err)
		}
		if err := checkout.Act(ctx, id, buyerID, "complete", ""); err != nil {
			t.Fatal(err)
		}
	}

	paid := purchase()
	balances(1900, 0, 0, 0, 0)
	complete(paid)
	balances(0, 1900, 0, 0, 0)
	var fee int64
	if err := pool.QueryRow(ctx, `SELECT l.units FROM journal_lines l JOIN ledger_accounts a ON a.id=l.account_id
		JOIN journal_entries e ON e.id=l.entry_id WHERE e.event_key='sale:'||$1::uuid AND a.bucket='commission'`, paid).Scan(&fee); err != nil || fee != 100 {
		t.Fatalf("commission or sale replay failed: %d %v", fee, err)
	}
	sellerClient.call("POST", "/payouts", url.Values{"amount": {"1.00"}}, 403)
	sellerClient.post("/payouts", url.Values{"key": {identity.NewToken()}, "amount": {"1.001"}, "currency": {"EUR"}, "scenario": {"success"}}, 422)
	if _, err := ledger.Request(ctx, sellerID, "USD", 1, identity.NewToken(), "success"); !errors.Is(err, finance.ErrFunds) {
		t.Fatalf("USD request consumed EUR: %v", err)
	}

	type result struct {
		id  pgtype.UUID
		err error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() {
			id, err := ledger.Request(ctx, sellerID, "EUR", 1200, identity.NewToken(), "success")
			results <- result{id, err}
		}()
	}
	first, second := <-results, <-results
	if first.err != nil {
		first, second = second, first
	}
	if first.err != nil || !errors.Is(second.err, finance.ErrFunds) {
		t.Fatalf("concurrent reservation: %v %v", first.err, second.err)
	}
	payout := first.id
	balances(0, 700, 0, 1200, 0)
	modPath := "/admin/payouts/" + payout.String() + "/action"
	moderator.call("GET", "/admin/payouts", nil, 403)
	moderator.post(modPath, url.Values{"action": {"review"}, "reason": {"Denied test"}}, 403)
	moderator.call("GET", "/payouts/"+payout.String(), nil, 404)
	browserAct := func(id pgtype.UUID, action string, want int) {
		t.Helper()
		admin.post("/admin/payouts/"+id.String()+"/action", url.Values{"action": {action}, "reason": {"Isolated finance test decision"}}, want)
	}
	browserAct(payout, "approve", 409)
	browserAct(payout, "review", 303)
	browserAct(payout, "deny", 303)
	balances(0, 1900, 0, 0, 0)

	request := func(amount, scenario, key string) pgtype.UUID {
		t.Helper()
		r := sellerClient.post("/payouts", url.Values{"key": {key}, "amount": {amount}, "currency": {"EUR"}, "scenario": {scenario}}, 303)
		var id pgtype.UUID
		if err := id.Scan(strings.TrimSuffix(strings.TrimPrefix(r.Header().Get("Location"), "/payouts/"), "?created=1")); err != nil {
			t.Fatal(err)
		}
		return id
	}
	key := identity.NewToken()
	retry := request("9.00", "fail_once", key)
	if repeated := request("9.00", "fail_once", key); repeated != retry {
		t.Fatal("payout retry created a second request")
	}
	browserAct(retry, "review", 303)
	browserAct(retry, "approve", 303)
	balances(0, 1000, 0, 900, 0)
	if _, err := q.PayoutReconciliation(ctx, retry); err == nil {
		t.Fatal("approval falsely reconciled provider transfer")
	}
	browserAct(retry, "process", 303)
	browserAct(retry, "observe", 303)
	if row, err := q.LockPayout(ctx, retry); err != nil || row.State != "failed" {
		t.Fatalf("failed payout scenario: %s %v", row.State, err)
	}
	balances(0, 1000, 0, 900, 0)
	browserAct(retry, "process", 303)
	browserAct(retry, "observe", 303)
	browserAct(retry, "observe", 303)
	balances(0, 1000, 0, 0, 900)
	browserAct(retry, "reconcile", 303)
	browserAct(retry, "reconcile", 303)
	if row, err := q.PayoutReconciliation(ctx, retry); err != nil || row.SettledUnits != 90000 {
		t.Fatalf("reconciliation units: %d %v", row.SettledUnits, err)
	}
	if body := sellerClient.call("GET", "/notifications", nil, 200).Body.String(); !strings.Contains(body, "/payouts/"+retry.String()) {
		t.Fatal("payout notice did not target seller record")
	}

	stalled := request("5.00", "stall_once", identity.NewToken())
	for _, action := range []string{"review", "approve", "process", "observe"} {
		browserAct(stalled, action, 303)
	}
	if row, err := q.LockPayout(ctx, stalled); err != nil || row.State != "stalled" {
		t.Fatalf("stalled payout scenario: %s %v", row.State, err)
	}
	balances(0, 500, 0, 500, 900)
	browserAct(stalled, "deny", 303)
	balances(0, 1000, 0, 0, 900)

	adjust := func(id pgtype.UUID, action string, want int) {
		t.Helper()
		admin.post("/admin/payment-adjustments/"+id.String(), url.Values{"action": {action}, "reason": {"Mock adjustment fixture"}}, want)
	}
	adjust(paid, "refund", 409)
	balances(0, 1000, 0, 0, 900)
	reversed := purchase()
	adjust(reversed, "reversal", 303)
	adjust(reversed, "reversal", 303)
	balances(0, 1000, 0, 0, 900)
	refunded := purchase()
	if err := checkout.Act(ctx, refunded, buyerID, "problem", ""); err != nil {
		t.Fatal(err)
	}
	balances(0, 1000, 1900, 0, 900)
	adjust(refunded, "refund", 303)
	adjust(refunded, "refund", 303)
	balances(0, 1000, 0, 0, 900)
	admin.call("GET", orderPath(refunded), nil, 404)

	var invalid int
	err = pool.QueryRow(ctx, `SELECT count(*) FROM (
		SELECT e.id FROM journal_entries e LEFT JOIN journal_lines l ON l.entry_id=e.id LEFT JOIN ledger_accounts a ON a.id=l.account_id
		GROUP BY e.id HAVING NOT e.sealed OR count(l.units)<2 OR sum(l.units)<>0 OR NOT bool_and(e.denomination=a.denomination)
	) invalid`).Scan(&invalid)
	if err != nil || invalid != 0 {
		t.Fatalf("unbalanced ledger: %d %v", invalid, err)
	}
	for _, statement := range []string{
		"UPDATE journal_lines SET units=units+1",
		"INSERT INTO journal_entries(event_key,subject_id,denomination,sealed) VALUES('fake-sealed',gen_random_uuid(),'EUR',true)",
		"INSERT INTO journal_entries(event_key,subject_id,denomination) VALUES('unsealed',gen_random_uuid(),'EUR')",
		"INSERT INTO journal_lines(entry_id,account_id,units) SELECT id,(SELECT id FROM ledger_accounts LIMIT 1),1 FROM journal_entries LIMIT 1",
		"UPDATE payout_reviews SET reason='rewritten'",
	} {
		if _, err := pool.Exec(ctx, statement); err == nil {
			t.Fatalf("database accepted invalid ledger write %q", statement)
		}
	}
}
