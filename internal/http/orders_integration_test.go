package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/x4342/marketplace/internal/db"
	"github.com/x4342/marketplace/internal/identity"
	"github.com/x4342/marketplace/internal/orders"
	"github.com/x4342/marketplace/internal/seller"
)

func TestOrdersPostgres(t *testing.T) {
	dsn := os.Getenv("MARKETPLACE_ORDERS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MARKETPLACE_ORDERS_TEST_DATABASE_URL to a migrated disposable test database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("cannot open test database")
	}
	defer pool.Close()
	queries := db.New(pool)
	accounts := identity.New(pool)
	service := orders.Simulation(pool)
	handler, err := New(queries, accounts, seller.Service{Pool: pool}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	// This fixture uses a disposable database so append-only logs and snapshots remain intact.
	buyer := testClient{t: t, handler: handler}
	merchant := testClient{t: t, handler: handler}
	outsider := testClient{t: t, handler: handler}
	for client, prefix := range map[*testClient]string{&buyer: "buyer", &merchant: "seller", &outsider: "other"} {
		var token [7]byte
		_, _ = rand.Read(token[:])
		client.call("GET", "/register", nil, 200)
		client.post("/register", url.Values{"username": {prefix[:min(len(prefix), 8)] + hex.EncodeToString(token[:])}, "password": {"isolated fixture passphrase"}}, 200)
	}

	b, err := accounts.Active(ctx, buyer.session)
	if err != nil {
		t.Fatal(err)
	}
	s, err := accounts.Active(ctx, merchant.session)
	if err != nil {
		t.Fatal(err)
	}

	newListing := func(title, kind, mode string, stock int) pgtype.UUID {
		t.Helper()
		category, condition, regions := "templates", "", []string{}
		if kind == "physical" {
			category, condition, regions = "accessories", "new", []string{"EU"}
		}
		var id pgtype.UUID
		err := pool.QueryRow(ctx, `INSERT INTO listings (seller_id,category_slug,title,description,kind,price_minor,currency,
			stock,condition,ship_regions,delivery_mode,terms,status,published_at)
			VALUES ($1,$2,$3,'Test description',$4,1250,'EUR',$5,$6,$7,$8,'Original terms','published',now()) RETURNING id`,
			s.ID, category, title, kind, stock, condition, regions, mode).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}

	checkout := func(id pgtype.UUID, key, region, address string) string {
		t.Helper()
		response := buyer.post("/listings/"+id.String()+"/checkout", url.Values{"key": {key}, "region": {region}, "address": {address}}, 303)
		path := response.Header().Get("Location")
		if !strings.HasPrefix(path, "/orders/") {
			t.Fatal("checkout did not create an order")
		}
		return path
	}
	act := func(client *testClient, path, action, details string, want int) {
		t.Helper()
		client.post(path+"/actions", url.Values{"action": {action}, "details": {details}}, want)
	}
	expectText := func(client *testClient, path, want string) {
		t.Helper()
		body := client.call("GET", path, nil, 200).Body.String()
		if !strings.Contains(body, want) {
			t.Fatalf("%s missing %q", path, want)
		}
	}

	physical := newListing("Fixture physical item", "physical", "", 2)
	physicalCheckout := "/listings/" + physical.String() + "/checkout"
	merchant.call("GET", physicalCheckout, nil, 409)
	buyer.call("POST", physicalCheckout, url.Values{"key": {identity.NewToken()}}, 403)
	buyer.post(physicalCheckout, url.Values{"key": {identity.NewToken()}, "region": {"US"}, "address": {"Test address"}}, 422)
	merchant.post("/sell/vacation", url.Values{"vacation": {"start"}}, 303)
	buyer.post(physicalCheckout, url.Values{"key": {identity.NewToken()}, "region": {"EU"}, "address": {"Test address"}}, 409)
	merchant.post("/sell/vacation", url.Values{"vacation": {"end"}}, 303)
	stock := func(id pgtype.UUID) int {
		t.Helper()
		var value int
		if err := pool.QueryRow(ctx, "SELECT stock FROM listings WHERE id=$1", id).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	key := identity.NewToken()
	physicalOrder := checkout(physical, key, "EU", "Fictional test address")
	if again := checkout(physical, key, "EU", "Fictional test address"); again != physicalOrder || stock(physical) != 1 {
		t.Fatal("retry changed the order or stock")
	}
	outsider.call("GET", physicalOrder, nil, 404)
	if prepayment := merchant.call("GET", physicalOrder, nil, 200).Body.String(); strings.Contains(prepayment, "Fictional test address") {
		t.Fatal("unpaid shipping address was exposed to the seller")
	}
	act(&merchant, physicalOrder, "simulate", "", 404)
	outsider.post(physicalOrder+"/actions", url.Values{"action": {"simulate"}}, 404)
	expectText(&buyer, physicalOrder, "TEST test units")
	physicalID := parseOrderID(t, physicalOrder)
	provider := &checkoutProbe{pool: pool}
	probed := service
	probed.Provider = provider
	_, err = probed.Create(ctx, orders.Checkout{Buyer: b.ID, Listing: physical, Key: identity.NewToken(), Region: "EU", Address: "Fictional second address"})
	if err != nil || !provider.sawCommittedOrder {
		t.Fatalf("provider did not observe a committed pre-quote order: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE listings SET title='Edited listing', price_minor=9999, terms='Edited terms' WHERE id=$1", physical); err != nil {
		t.Fatal(err)
	}
	merchant.post("/sell/listings/"+physical.String()+"/status", url.Values{"status": {"paused"}}, 303)
	merchant.post("/sell/vacation", url.Values{"vacation": {"start"}}, 303)
	buyer.post(physicalCheckout, url.Values{"key": {identity.NewToken()}, "region": {"EU"}, "address": {"Test address"}}, 409)
	expectText(&buyer, physicalOrder, "Fixture physical item")
	expectText(&buyer, physicalOrder, "EUR 12.50")
	expectText(&buyer, physicalOrder, "Original terms")

	for name, modify := range map[string]func(*db.StorePaymentObservationParams){
		"underpaid":     func(o *db.StorePaymentObservationParams) { o.Units-- },
		"overpaid":      func(o *db.StorePaymentObservationParams) { o.Units++ },
		"wrong network": func(o *db.StorePaymentObservationParams) { o.Network = "wrong-network" },
		"wrong asset":   func(o *db.StorePaymentObservationParams) { o.Asset = "WRONG" },
	} {
		provider.modify = modify
		if err := probed.SimulatePayment(ctx, physicalID, b.ID); !errors.Is(err, orders.ErrQuote) {
			t.Fatalf("%s returned %v", name, err)
		}
	}
	provider.modify = nil
	var threadCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM conversations WHERE listing_id=$1", physical).Scan(&threadCount); err != nil || threadCount != 0 {
		t.Fatal("fixture started with a pre-existing conversation")
	}
	var group sync.WaitGroup
	errs := make(chan error, 6)
	for index := 0; index < cap(errs); index++ {
		group.Add(1)
		go func() { defer group.Done(); errs <- service.SimulatePayment(ctx, physicalID, b.ID) }()
	}
	group.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var purchaseCount, paymentCount, paidCount int
	var thread string
	err = pool.QueryRow(ctx, `SELECT count(*), min(conversation_id::text)
		FROM conversation_system_events WHERE order_id=$1 AND title_snapshot='Fixture physical item'`, physicalID).Scan(&purchaseCount, &thread)
	if err != nil || purchaseCount != 1 {
		t.Fatalf("purchase event count %d (%v)", purchaseCount, err)
	}
	err = pool.QueryRow(ctx, "SELECT count(*) FROM payment_observations WHERE order_id=$1", physicalID).Scan(&paymentCount)
	if err != nil || paymentCount != 1 {
		t.Fatalf("observation count %d (%v)", paymentCount, err)
	}
	err = pool.QueryRow(ctx, "SELECT count(*) FROM order_status_events WHERE order_id=$1 AND state='paid'", physicalID).Scan(&paidCount)
	if err != nil || paidCount != 1 {
		t.Fatalf("paid transition count %d (%v)", paidCount, err)
	}
	expectText(&buyer, "/messages/"+thread, "Fixture physical item")
	expectText(&merchant, "/messages/"+thread, "Bought")
	outsider.call("GET", "/messages/"+thread, nil, 404)
	expectText(&merchant, physicalOrder, "Fictional test address")
	expectText(&buyer, physicalOrder, "/messages/"+thread)
	act(&buyer, physicalOrder, "dispatch", "", 409)
	act(&merchant, physicalOrder, "dispatch", "TEST-TRACK", 303)
	expectText(&buyer, physicalOrder, "Tracking reference TEST-TRACK")
	act(&buyer, physicalOrder, "complete", "", 303)
	expectText(&buyer, physicalOrder, "Completed")
	merchant.post("/sell/vacation", url.Values{"vacation": {"end"}}, 303)

	automatic := newListing("Fixture automatic delivery", "digital", "automatic", 3)
	assetPath := "/sell/listings/" + automatic.String() + "/download"
	outsider.post(assetPath, url.Values{"payload": {"unrelated payload"}}, 404)
	buyer.call("GET", "/listings/"+automatic.String()+"/checkout", nil, 409)
	merchant.post(assetPath, url.Values{"payload": {"Original private delivery."}}, 303)
	automaticOrder := checkout(automatic, identity.NewToken(), "", "")
	buyer.call("GET", automaticOrder+"/download", nil, 404)
	merchant.post(assetPath, url.Values{"payload": {"Replacement for future purchases."}}, 303)
	act(&buyer, automaticOrder, "simulate", "", 303)
	act(&buyer, automaticOrder, "simulate", "", 303)
	download := buyer.call("GET", automaticOrder+"/download", nil, 200)
	if download.Body.String() != "Original private delivery." || download.Header().Get("Cache-Control") != "no-store" ||
		!strings.HasPrefix(download.Header().Get("Content-Disposition"), "attachment") {
		t.Fatal("download snapshot, caching, or attachment behavior failed")
	}
	outsider.call("GET", automaticOrder+"/download", nil, 404)
	merchant.call("GET", automaticOrder+"/download", nil, 404)
	expectText(&buyer, "/notifications", automaticOrder+"/download")
	act(&buyer, automaticOrder, "complete", "", 303)

	manual := newListing("Fixture manual delivery", "digital", "manual", 2)
	openPath := "/listings/" + manual.String() + "/conversation"
	existingThread := buyer.post(openPath, url.Values{}, 303).Header().Get("Location")
	manualOrder := checkout(manual, identity.NewToken(), "", "")
	act(&buyer, manualOrder, "simulate", "", 303)
	expectText(&merchant, manualOrder, "Test deadline")
	expectText(&merchant, "/notifications", manualOrder+"#manual-delivery")
	expectText(&buyer, existingThread, "Bought")
	act(&buyer, manualOrder, "deliver", "private fixture", 409)
	act(&merchant, manualOrder, "deliver", "Private manual delivery", 303)
	expectText(&buyer, manualOrder, "Private manual delivery")
	outsider.call("GET", manualOrder, nil, 404)
	act(&buyer, manualOrder, "problem", "", 303)
	expectText(&buyer, manualOrder, "Problem reported")

	failedOrder := checkout(manual, identity.NewToken(), "", "")
	act(&buyer, failedOrder, "simulate", "", 303)
	act(&merchant, failedOrder, "failed", "", 303)
	expectText(&buyer, failedOrder, "Fulfillment failed")
	act(&buyer, failedOrder, "problem", "", 303)

	pending := newListing("Fixture stock reservation", "digital", "manual", 1)
	cancelled := checkout(pending, identity.NewToken(), "", "")
	if stock(pending) != 0 {
		t.Fatal("checkout did not reserve stock")
	}
	act(&buyer, cancelled, "cancel", "", 303)
	act(&buyer, cancelled, "cancel", "", 303)
	if stock(pending) != 1 {
		t.Fatal("cancellation did not release stock exactly once")
	}
	expiring := checkout(pending, identity.NewToken(), "", "")
	expiringID := parseOrderID(t, expiring)
	if _, err := pool.Exec(ctx, "UPDATE orders SET checkout_deadline=now()-interval '1 minute' WHERE id=$1", expiringID); err != nil {
		t.Fatal(err)
	}
	if err := service.SweepExpired(ctx); err != nil {
		t.Fatal(err)
	}
	if err := service.SweepExpired(ctx); err != nil {
		t.Fatal(err)
	}
	act(&buyer, expiring, "simulate", "", 409)
	expectText(&buyer, expiring, "Expired")
	if stock(pending) != 1 {
		t.Fatal("expiry did not release stock exactly once")
	}

	last := newListing("Fixture one available unit", "digital", "manual", 1)
	keys := []string{identity.NewToken(), identity.NewToken()}
	outcomes := make(chan error, 2)
	for _, unique := range keys {
		go func() {
			_, err := service.Create(ctx, orders.Checkout{Buyer: b.ID, Listing: last, Key: unique})
			outcomes <- err
		}()
	}
	first, second := <-outcomes, <-outcomes
	if stock(last) != 0 || !((first == nil && errors.Is(second, orders.ErrUnavailable)) ||
		(second == nil && errors.Is(first, orders.ErrUnavailable))) {
		t.Fatalf("competing checkouts oversold inventory or failed unexpectedly: %v and %v", first, second)
	}
	if _, err := pool.Exec(ctx, "UPDATE order_items SET title='Mutated' WHERE order_id=$1", physicalID); err == nil {
		t.Fatal("database accepted a snapshot update")
	}
}

type checkoutProbe struct {
	orders.MockProvider
	pool              *pgxpool.Pool
	sawCommittedOrder bool
	modify            func(*db.StorePaymentObservationParams)
}

func (p *checkoutProbe) Quote(ctx context.Context, id pgtype.UUID, cents int64, deadline time.Time) (db.StoreQuoteParams, error) {
	order, err := db.New(p.pool).LockOrder(ctx, id)
	if err != nil {
		return db.StoreQuoteParams{}, err
	}
	_, quoteErr := db.New(p.pool).OrderQuote(ctx, id)
	if order.State != "awaiting_payment" || order.PriceMinor != cents || !errors.Is(quoteErr, pgx.ErrNoRows) {
		return db.StoreQuoteParams{}, fmt.Errorf("provider invoked without an unpaid order snapshot and absent quote")
	}
	p.sawCommittedOrder = true
	return p.MockProvider.Quote(ctx, id, cents, deadline)
}

func (p *checkoutProbe) Observe(ctx context.Context, q db.Quote) (db.StorePaymentObservationParams, error) {
	observation, err := p.MockProvider.Observe(ctx, q)
	if p.modify != nil {
		p.modify(&observation)
	}
	return observation, err
}

func parseOrderID(t *testing.T, path string) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	if err := id.Scan(strings.TrimPrefix(path, "/orders/")); err != nil {
		t.Fatal(err)
	}
	return id
}
