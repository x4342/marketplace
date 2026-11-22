package web

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"image"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/x4342/marketplace/internal/db"
	"github.com/x4342/marketplace/internal/identity"
	"github.com/x4342/marketplace/internal/seller"
)

func TestSellerPostgres(t *testing.T) {
	dsn := os.Getenv("MARKETPLACE_SELLER_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MARKETPLACE_SELLER_TEST_DATABASE_URL to a migrated, seeded test database")
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

	var random [8]byte
	_, _ = rand.Read(random[:])
	suffix := hex.EncodeToString(random[:])
	ownerName, otherName := "seller_"+suffix, "other_"+suffix
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM listings WHERE seller_id IN (SELECT id FROM users WHERE username IN ($1, $2))", ownerName, otherName)
		_, _ = pool.Exec(ctx, "DELETE FROM security_events WHERE user_id IN (SELECT id FROM users WHERE username IN ($1, $2))", ownerName, otherName)
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE username IN ($1, $2)", ownerName, otherName)
	}()
	owner := testClient{t: t, handler: handler}
	other := testClient{t: t, handler: handler}
	guest := testClient{t: t, handler: handler}
	for client, name := range map[*testClient]string{&owner: ownerName, &other: otherName} {
		client.call("GET", "/register", nil, 200)
		client.post("/register", url.Values{"username": {name}, "password": {"isolated fixture passphrase"}}, 200)
	}
	guest.call("GET", "/sell", nil, 303)
	owner.call("POST", "/sell/new", url.Values{"title": {"CSRF fixture"}}, 403)
	saved := owner.post("/sell/new", url.Values{"title": {"Unfinished " + suffix}}, 303)
	draft := strings.TrimSuffix(saved.Header().Get("Location"), "?saved=1")
	id := strings.TrimPrefix(draft, "/sell/drafts/")
	if id == draft || id == "" {
		t.Fatal("draft save did not return a listing ID")
	}
	public := "/listings/" + id

	if !strings.Contains(owner.call("GET", draft+"?saved=1", nil, 200).Body.String(), "Draft saved.") {
		t.Fatal("save feedback missing")
	}
	if !strings.Contains(owner.call("GET", draft+"/preview", nil, 200).Body.String(), "Choose a current category.") {
		t.Fatal("incomplete draft preview did not show publication requirements")
	}
	owner.post(draft+"/publish", url.Values{"revision": {"1"}}, 303)
	guest.call("GET", public, nil, 404)
	other.call("GET", draft, nil, 404)
	other.post(draft+"/publish", url.Values{"revision": {"1"}}, 404)

	title := "Publication fixture " + suffix
	fields := url.Values{
		"revision": {"1"}, "action": {"preview"}, "title": {title},
		"description": {"Fixture description"}, "category": {"accessories"}, "kind": {"physical"},
		"price": {"12.50"}, "currency": {"EUR"}, "stock": {"2"}, "condition": {"used"},
		"region_eu": {"1"}, "terms": {"Ships in two days"},
	}
	rejected := postListingImage(t, &owner, draft, fields, []byte("<svg/>"), 422)
	if !strings.Contains(rejected.Body.String(), title) || !strings.Contains(rejected.Body.String(), "Upload a JPEG or PNG") {
		t.Fatal("invalid image response lost form content or upload feedback")
	}
	var imageBytes bytes.Buffer
	if err := png.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 10, 10))); err != nil {
		t.Fatal(err)
	}
	postListingImage(t, &owner, draft, fields, imageBytes.Bytes(), 303)
	owner.post(draft, fields, 409)
	owner.call("GET", draft+"/image", nil, 200)
	other.call("GET", draft+"/image", nil, 404)
	guest.call("GET", public+"/image", nil, 404)
	preview := owner.call("GET", draft+"/preview", nil, 200).Body.String()
	if !strings.Contains(preview, "Mobile preview") || !strings.Contains(preview, "Publish listing") {
		t.Fatal("valid preview is missing presentation or publish controls")
	}
	owner.post(draft+"/publish", url.Values{"revision": {"2"}}, 303)
	owner.call("GET", draft, nil, 404)
	if img := guest.call("GET", public+"/image", nil, 200); img.Header().Get("Content-Type") != "image/jpeg" || !bytes.HasPrefix(img.Body.Bytes(), []byte{0xff, 0xd8}) {
		t.Fatal("published image was not served as re-encoded JPEG")
	}
	if body := guest.call("GET", public, nil, 200).Body.String(); !strings.Contains(body, "EUR 12.50") || !strings.Contains(body, title) {
		t.Fatal("publication did not expose valid listing fields")
	}

	other.post("/sell/listings/"+id+"/edit", url.Values{}, 404)
	owner.post("/sell/listings/"+id+"/edit", url.Values{}, 303)
	fields.Set("price", "14.25")
	owner.post(draft, fields, 303)
	if body := guest.call("GET", public, nil, 200).Body.String(); !strings.Contains(body, "EUR 12.50") {
		t.Fatal("editing a draft modified the existing publication")
	}
	owner.post(draft+"/publish", url.Values{"revision": {"2"}}, 303)
	if body := guest.call("GET", public, nil, 200).Body.String(); !strings.Contains(body, "EUR 14.25") {
		t.Fatal("publishing an edit did not update public price")
	}
	owner.post("/sell/vacation", url.Values{"vacation": {"start"}}, 303)
	if body := guest.call("GET", public, nil, 200).Body.String(); !strings.Contains(body, "Seller away") {
		t.Fatal("vacation mode was not visible on the public listing")
	}
	owner.post("/sell/vacation", url.Values{"vacation": {"end"}}, 303)

	status := "/sell/listings/" + id + "/status"
	other.post(status, url.Values{"status": {"paused"}}, 404)
	owner.post(status, url.Values{"status": {"paused"}}, 303)
	guest.call("GET", public, nil, 404)
	guest.call("GET", public+"/image", nil, 404)
	owner.post(status, url.Values{"status": {"published"}}, 303)
	guest.call("GET", public, nil, 200)
	owner.post(status, url.Values{"status": {"removed"}}, 303)
	guest.call("GET", public, nil, 404)
	owner.post(status, url.Values{"status": {"published"}}, 404)
}

func postListingImage(t *testing.T, client *testClient, path string, fields url.Values, content []byte, want int) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("csrf", client.csrf); err != nil {
		t.Fatal(err)
	}
	for name := range fields {
		if err := writer.WriteField(name, fields.Get(name)); err != nil {
			t.Fatal(err)
		}
	}
	file, err := writer.CreateFormFile("image", "fixture.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: csrfCookie, Value: client.csrf})
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: client.session})
	response := httptest.NewRecorder()
	client.handler.ServeHTTP(response, req)
	if response.Code != want {
		t.Fatalf("upload returned %d, want %d", response.Code, want)
	}
	return response
}
