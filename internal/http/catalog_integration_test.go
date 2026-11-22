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
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/x4342/marketplace/internal/db"
	"github.com/x4342/marketplace/internal/identity"
	"github.com/x4342/marketplace/internal/seller"
)

func TestCatalogPostgres(t *testing.T) {
	dsn := os.Getenv("MARKETPLACE_CATALOG_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MARKETPLACE_CATALOG_TEST_DATABASE_URL to a migrated, seeded test database")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal("cannot open test database")
	}
	defer pool.Close()

	handler, err := New(db.New(pool), identity.New(pool), seller.Service{Pool: pool}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, path   string
		status       int
		show, absent string
	}{
		{"index", "/discover", 200, "Compact mechanical keyboard", "Private fixture draft"},
		{"keyword with parent category", "/discover?q=keyboard&category=equipment", 200, "Cork desk mat", "Workshop inventory sheets"},
		{"in stock", "/discover?availability=in_stock", 200, "Compact mechanical keyboard", "Cork desk mat"},
		{"orderable", "/discover?availability=orderable", 200, "Cork desk mat", "Compact mechanical keyboard"},
		{"fx filter", "/discover?currency=USD&min=63&max=64", 200, "Compact mechanical keyboard", "Cork desk mat"},
		{"invalid price", "/discover?min=30&max=10", 200, "Minimum price must be no greater", "Cork desk mat"},
		{"listing", "/listings/892351c3-15c9-468e-a61f-b948658c18e5", 200, "Ships within two working days", "Private fixture draft"},
		{"hidden draft", "/listings/ced4424d-b34f-40a3-80cc-48933aab997c", 404, "Page unavailable", "Private fixture draft"},
		{"invalid ID", "/listings/invalid", 404, "Page unavailable", "ERROR:"},
		{"storefront", "/stores/workshop", 200, "Cork desk mat", "Workshop inventory sheets"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, tt.path, nil))
			body := recorder.Body.String()
			if recorder.Code != tt.status || !strings.Contains(body, tt.show) || strings.Contains(body, tt.absent) {
				t.Fatalf("%s: got HTTP %d, want %d, expected %q, forbidden %q", tt.path, recorder.Code, tt.status, tt.show, tt.absent)
			}
		})
	}
	t.Run("normalized sorting and pagination", func(t *testing.T) {
		ctx := context.Background()
		_, err := pool.Exec(ctx, `INSERT INTO listings
			(seller_id, category_slug, title, description, kind, price_minor, currency, stock, delivery_mode, terms, status, published_at)
			SELECT id, 'templates', 'Paging fixture ' || series, 'Catalog test listing.', 'digital',
			       series * 100, 'USD', 1, 'manual', 'Fixture', 'published', now()
			FROM users CROSS JOIN generate_series(1, 13) AS series WHERE username = 'papertrail'`)
		if err != nil {
			t.Fatal(err)
		}
		defer pool.Exec(ctx, "DELETE FROM listings WHERE title LIKE 'Paging fixture %'")
		first := httptest.NewRecorder()
		handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/discover?q=Paging&sort=price_desc", nil))
		firstBody := first.Body.String()
		if first.Code != 200 || !strings.Contains(firstBody, "13 listings") ||
			!strings.Contains(firstBody, "page=2&amp;q=Paging&amp;sort=price_desc") ||
			strings.Contains(firstBody, ">Paging fixture 1<") {
			t.Fatal("first page did not preserve search/sort or apply the 12-item boundary")
		}
		second := httptest.NewRecorder()
		handler.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/discover?page=2&q=Paging&sort=price_desc", nil))
		secondBody := second.Body.String()
		if second.Code != 200 || !strings.Contains(secondBody, ">Paging fixture 1<") ||
			strings.Contains(secondBody, ">Paging fixture 13<") || !strings.Contains(secondBody, "Previous page") ||
			!strings.Contains(secondBody, `href="/discover?q=Paging">Reset all`) {
			t.Fatal("second page or reset did not preserve the expected keyword and sort state")
		}
	})
	t.Run("owner-scoped favorites", func(t *testing.T) {
		var suffix [8]byte
		_, _ = rand.Read(suffix[:])
		key := hex.EncodeToString(suffix[:])
		buyer, other := "buyer_"+key, "other_"+key
		defer func() {
			_, _ = pool.Exec(context.Background(), "DELETE FROM security_events WHERE user_id IN (SELECT id FROM users WHERE username IN ($1, $2))", buyer, other)
			_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE username IN ($1, $2)", buyer, other)
		}()
		one := testClient{t: t, handler: handler}
		two := testClient{t: t, handler: handler}
		one.call("GET", "/discover?saved=1", nil, 303)
		for client, username := range map[*testClient]string{&one: buyer, &two: other} {
			client.call("GET", "/register", nil, 200)
			client.post("/register", url.Values{"username": {username}, "password": {"test fixture password"}}, 200)
		}
		path := "/listings/892351c3-15c9-468e-a61f-b948658c18e5/favorite"
		one.call("POST", path, url.Values{"favorite": {"save"}}, 403)
		card := one.call("GET", "/discover?q=keyboard", nil, 200).Body.String()
		if !strings.Contains(card, "Save Compact mechanical keyboard") || !strings.Contains(card, "/discover?q=keyboard") {
			t.Fatal("signed-in discovery results lack the save control")
		}
		response := one.post(path, url.Values{"favorite": {"save"}, "return": {"/discover?q=keyboard"}}, 303)
		if response.Header().Get("Location") != "/discover?q=keyboard" {
			t.Fatal("saving a card lost its discovery filters")
		}
		response = one.post(path, url.Values{"favorite": {"save"}, "return": {"//outside.invalid/discover"}}, 303)
		if response.Header().Get("Location") != strings.TrimSuffix(path, "/favorite") {
			t.Fatal("favorite action accepted an external redirect")
		}
		two.post(path, url.Values{"favorite": {"remove"}}, 303)
		if body := one.call("GET", "/discover?saved=1", nil, 200).Body.String(); !strings.Contains(body, "Compact mechanical keyboard") {
			t.Fatal("favorite did not persist for its owner")
		}
		if body := two.call("GET", "/discover?saved=1", nil, 200).Body.String(); strings.Contains(body, "Compact mechanical keyboard") {
			t.Fatal("favorite displayed in another account")
		}
		one.post(path, url.Values{"favorite": {"remove"}}, 303)
		if body := one.call("GET", "/discover?saved=1", nil, 200).Body.String(); strings.Contains(body, "Compact mechanical keyboard") {
			t.Fatal("favorite was not removed")
		}
	})

}
