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
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/x4342/marketplace/internal/db"
	"github.com/x4342/marketplace/internal/identity"
	"github.com/x4342/marketplace/internal/seller"
)

func TestMessagesPostgres(t *testing.T) {
	dsn := os.Getenv("MARKETPLACE_MESSAGES_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MARKETPLACE_MESSAGES_TEST_DATABASE_URL to a migrated, seeded test database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("cannot open test database")
	}
	defer pool.Close()
	handler, err := New(db.New(pool), identity.New(pool), seller.Service{Pool: pool}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}

	var token [7]byte
	_, _ = rand.Read(token[:])
	suffix := hex.EncodeToString(token[:])
	buyerName, sellerName, guestName := "buyer"+suffix, "seller"+suffix, "other"+suffix
	names := []string{buyerName, sellerName, guestName}
	defer func() {
		tables := []string{"notifications", "reports", "conversation_blocks", "conversation_reads", "messages"}
		for _, table := range tables {
			_, _ = pool.Exec(ctx, "DELETE FROM "+table+" WHERE conversation_id IN (SELECT c.id FROM conversations c JOIN users u ON u.id = c.buyer_id WHERE u.username = $1)", buyerName)
		}
		_, _ = pool.Exec(ctx, "DELETE FROM conversations WHERE buyer_id IN (SELECT id FROM users WHERE username = $1)", buyerName)
		_, _ = pool.Exec(ctx, "DELETE FROM listings WHERE seller_id IN (SELECT id FROM users WHERE username = $1)", sellerName)
		_, _ = pool.Exec(ctx, "DELETE FROM security_events WHERE user_id IN (SELECT id FROM users WHERE username = ANY($1::text[]))", names)
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE username = ANY($1::text[])", names)
	}()
	buyer := testClient{t: t, handler: handler}
	merchant := testClient{t: t, handler: handler}
	outsider := testClient{t: t, handler: handler}
	for client, name := range map[*testClient]string{&buyer: buyerName, &merchant: sellerName, &outsider: guestName} {
		client.call("GET", "/register", nil, 200)
		client.post("/register", url.Values{"username": {name}, "password": {"isolated test password"}}, 200)
	}

	var listingID string
	err = pool.QueryRow(ctx, `INSERT INTO listings (seller_id, category_slug, title, description, kind, price_minor, currency,
		stock, delivery_mode, terms, status, published_at)
		SELECT id, 'templates', 'Message fixture', 'Digital test listing.', 'digital', 1200, 'USD', 3, 'manual', 'Test', 'published', now()
		FROM users WHERE username = $1 RETURNING id::text`, sellerName).Scan(&listingID)
	if err != nil {
		t.Fatal(err)
	}
	openPath := "/listings/" + listingID + "/conversation"
	merchant.post(openPath, url.Values{}, 404)
	buyer.call("POST", openPath, nil, 403)
	thread := buyer.post(openPath, url.Values{}, 303).Header().Get("Location")
	if thread == "" || thread != buyer.post(openPath, url.Values{}, 303).Header().Get("Location") {
		t.Fatal("contacting the same seller did not reuse its listing conversation")
	}
	threadID := strings.TrimPrefix(thread, "/messages/")
	outsider.call("GET", thread, nil, 404)
	outsider.call("GET", thread+"/entries", nil, 404)
	outsider.post(thread+"/send", url.Values{"message": {"intrusion fixture"}}, 404)
	outsider.post(thread+"/block", url.Values{"block": {"block"}}, 404)

	buyer.post(thread+"/send", url.Values{"message": {"\u003cscript\u003eHello\u003c/script\u003e"}, "kind": {"purchase"}, "order_id": {listingID}}, 303)
	page := merchant.call("GET", thread, nil, 200).Body.String()
	if !strings.Contains(page, "&lt;script&gt;Hello&lt;/script&gt;") || strings.Contains(page, "<script>Hello</script>") {
		t.Fatal("normal text did not remain escaped")
	}
	var events int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM conversation_system_events WHERE conversation_id = $1", threadID).Scan(&events); err != nil || events != 0 {
		t.Fatal("message submission wrote a purchase event")
	}

	var noticeID string
	var seq int64
	err = pool.QueryRow(ctx, `SELECT n.id::text, m.sequence FROM notifications n JOIN messages m ON m.id = n.message_id
		WHERE n.conversation_id = $1`, threadID).Scan(&noticeID, &seq)
	if err != nil {
		t.Fatal(err)
	}
	if inbox := merchant.call("GET", "/messages", nil, 200).Body.String(); !strings.Contains(inbox, "Unread messages") {
		t.Fatal("recipient inbox did not show unread activity")
	}
	noticePath := "/notifications/" + noticeID
	outsider.call("GET", noticePath, nil, 404)
	buyer.call("GET", noticePath, nil, 404)
	link := merchant.call("GET", noticePath, nil, 303).Header().Get("Location")
	if !strings.HasPrefix(link, thread+"?before=") || !strings.Contains(link, "#message-") {
		t.Fatal("notification did not target the correct message")
	}
	merchant.post(thread+"/read", url.Values{"sequence": {strconv.FormatInt(seq, 10)}}, 303)
	if inbox := merchant.call("GET", "/messages", nil, 200).Body.String(); strings.Contains(inbox, "Unread messages") {
		t.Fatal("mark as read did not update the recipient state")
	}

	merchant.post(thread+"/block", url.Values{"block": {"block"}}, 303)
	buyer.post(thread+"/send", url.Values{"message": {"message while blocked"}}, 403)
	buyer.post(thread+"/block", url.Values{"block": {"unblock"}}, 303)
	buyer.post(thread+"/send", url.Values{"message": {"cannot remove someone else's block"}}, 403)
	merchant.post(thread+"/report", url.Values{"reason": {"Fixture report"}}, 303)
	var reports int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM reports WHERE conversation_id = $1", threadID).Scan(&reports); err != nil || reports != 1 {
		t.Fatal("the participant report was not persisted")
	}
	merchant.post(thread+"/block", url.Values{"block": {"unblock"}}, 303)
	merchant.post("/sell/vacation", url.Values{"vacation": {"start"}}, 303)
	merchant.post("/sell/listings/"+listingID+"/status", url.Values{"status": {"paused"}}, 303)
	buyer.post(openPath, url.Values{}, 404)
	buyer.post(thread+"/send", url.Values{"message": {"existing thread still works"}}, 303)
	if fragment := merchant.call("GET", thread+"/entries", nil, 200).Body.String(); !strings.Contains(fragment, "existing thread still works") {
		t.Fatal("paused listing or vacation interrupted the existing conversation")
	}

	_, err = pool.Exec(ctx, `INSERT INTO messages (conversation_id, sender_id, body, sequence)
		SELECT c.id, c.buyer_id, 'History fixture ' || n,
		    n + (SELECT coalesce(max(sequence), 0) FROM messages WHERE conversation_id = c.id)
		FROM conversations c
		CROSS JOIN generate_series(1, 31) n WHERE c.id = $1`, threadID)
	if err != nil {
		t.Fatal(err)
	}
	if newest := merchant.call("GET", thread, nil, 200).Body.String(); !strings.Contains(newest, "Older messages") || strings.Contains(newest, "History fixture 1</p>") {
		t.Fatal("the latest page did not apply the history boundary")
	}
	if older := merchant.call("GET", strings.Split(link, "#")[0], nil, 200).Body.String(); !strings.Contains(older, "&lt;script&gt;Hello&lt;/script&gt;") {
		t.Fatal("notification pagination lost the target message")
	}
}

func TestDirectMessagesPostgres(t *testing.T) {
	dsn := os.Getenv("MARKETPLACE_MESSAGES_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires a migrated disposable database")
	}
	ctx := t.Context()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	auth := identity.New(pool)
	handler, err := New(db.New(pool), auth, seller.Service{Pool: pool}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	var suffix [4]byte
	_, _ = rand.Read(suffix[:])
	aName, bName, cName := "directa"+hex.EncodeToString(suffix[:]), "directb"+hex.EncodeToString(suffix[:]), "directc"+hex.EncodeToString(suffix[:])
	a, b, c := testClient{t: t, handler: handler}, testClient{t: t, handler: handler}, testClient{t: t, handler: handler}
	for client, name := range map[*testClient]string{&a: aName, &b: bName, &c: cName} {
		client.call("GET", "/register", nil, 200)
		client.post("/register", url.Values{"username": {name}, "password": {"sample direct fixture password"}}, 200)
	}
	guest := testClient{t: t, handler: handler}
	search := "/discover?search=users&q=" + strings.ToUpper(bName) + "&type=invalid"
	if body := guest.call("GET", search, nil, 200).Body.String(); !strings.Contains(body, bName) || strings.Contains(body, "Sort by") || strings.Contains(body, "/messages/direct") {
		t.Fatal("unexpected anonymous member search")
	}
	if body := a.call("GET", search, nil, 200).Body.String(); !strings.Contains(body, "Message "+bName) {
		t.Fatal("member search omitted the message action")
	}
	if body := b.call("GET", "/users/"+bName, nil, 200).Body.String(); strings.Contains(body, "Message "+bName) {
		t.Fatal("self message action rendered")
	}
	a.call("POST", "/messages/direct", nil, 403)
	a.post("/messages/direct", url.Values{"username": {aName}}, 404)
	paths := make(chan string, 2)
	start := func(client testClient, partner string) {
		writer := httptest.NewRecorder()
		form := url.Values{"username": {partner}, "csrf": {client.csrf}}
		req := httptest.NewRequest("POST", "/messages/direct", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: client.session})
		req.AddCookie(&http.Cookie{Name: csrfCookie, Value: client.csrf})
		handler.ServeHTTP(writer, req)
		if writer.Code != 303 {
			paths <- ""
			return
		}
		paths <- writer.Header().Get("Location")
	}
	go start(a, strings.ToUpper(bName))
	go start(b, aName)
	path, reversed := <-paths, <-paths
	if path == "" || path != reversed {
		t.Fatal("concurrent reversed start did not use one direct thread")
	}
	c.call("GET", path, nil, 404)
	c.call("GET", path+"/entries", nil, 404)
	c.post(path+"/send", url.Values{"message": {"intrusion"}}, 404)
	a.post(path+"/send", url.Values{"message": {"Private direct fixture"}, "kind": {"purchase"}}, 303)
	if body := b.call("GET", "/messages", nil, 200).Body.String(); !strings.Contains(body, "Private direct fixture") || !strings.Contains(body, `aria-current="page"`) {
		t.Fatal("inbox did not open the latest direct conversation")
	}
	pathID := strings.TrimPrefix(path, "/messages/")
	var events int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM conversation_system_events WHERE conversation_id=$1", pathID).Scan(&events); err != nil || events != 0 {
		t.Fatal("direct send created a purchase event", err)
	}
	b.post(path+"/block", url.Values{"block": {"block"}}, 303)
	a.post(path+"/send", url.Values{"message": {"blocked fixture"}}, 403)
	b.post(path+"/block", url.Values{"block": {"unblock"}}, 303)
	b.post(path+"/report", url.Values{"reason": {"Synthetic direct case"}}, 303)
	if _, err := pool.Exec(ctx, `INSERT INTO user_restrictions(user_id,moderator_id,reason)
		SELECT u.id,m.id,'synthetic restriction' FROM users u,users m WHERE u.username=$1 AND m.username=$2`, bName, cName); err != nil {
		t.Fatal(err)
	}
	a.post("/messages/direct", url.Values{"username": {bName}}, 404)
	a.post(path+"/send", url.Values{"message": {"restricted peer fixture"}}, 403)
	if body := guest.call("GET", search, nil, 200).Body.String(); strings.Contains(body, "/users/"+bName) {
		t.Fatal("restricted account leaked in member search")
	}
	guest.call("GET", "/users/"+bName, nil, 404)
}
