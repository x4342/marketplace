package web

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"image/png"
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

func TestProfilesPostgres(t *testing.T) {
	dsn := os.Getenv("MARKETPLACE_PROFILE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires a disposable migrated PostgreSQL database")
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
	owner := testClient{t: t, handler: handler}
	guest := testClient{t: t, handler: handler}
	other := testClient{t: t, handler: handler}
	name := "profile_" + strings.ToLower(identity.NewToken()[:10])
	name = strings.NewReplacer("-", "a", "_", "b").Replace(name)
	const password = "a profile test passphrase"
	owner.call("GET", "/register", nil, 200)
	owner.post("/register", url.Values{"username": {name}, "password": {password}}, 200)
	viewer, err := accounts.Register(ctx, name+"_viewer", password)
	if err != nil {
		t.Fatal(err)
	}
	other.session = viewer.Session
	other.call("GET", "/account/profile", nil, 200)
	public := "/users/" + name
	guest.call("GET", public, nil, 200)
	guest.call("GET", "/account/profile", nil, 303)
	if avatar := guest.call("GET", public+"/avatar", nil, 200); avatar.Header().Get("Content-Type") != "image/svg+xml" {
		t.Fatal("default avatar unavailable")
	}
	guest.call("POST", "/account/profile", nil, 303)
	owner.call("POST", "/account/profile", url.Values{"bio": {"Missing CSRF"}}, 403)
	other.post("/account/profile", url.Values{"username": {name}, "bio": {"Somebody else's profile"}}, 303)
	var source bytes.Buffer
	if err := png.Encode(&source, image.NewRGBA(image.Rect(0, 0, 640, 320))); err != nil {
		t.Fatal(err)
	}
	const bio = "Profile biography <script>private test</script>"
	postListingImage(t, &owner, "/account/profile", url.Values{"bio": {bio}}, source.Bytes(), 303)
	for _, client := range []*testClient{&guest, &other} {
		page := client.call("GET", public, nil, 200).Body.String()
		if !strings.Contains(page, "Profile biography &lt;script&gt;") || strings.Contains(page, password) || strings.Contains(page, "Somebody else") || strings.Contains(page, "Recovery codes") {
			t.Fatal("incorrect public profile projection")
		}
	}
	picture := guest.call("GET", public+"/avatar", nil, 200)
	conf, err := jpeg.DecodeConfig(picture.Body)
	if err != nil || conf.Width != 256 || conf.Height != 256 || picture.Body.Len() >= 512*1024 {
		t.Fatal("invalid public avatar")
	}
	actor, err := accounts.Active(ctx, owner.session)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "INSERT INTO user_restrictions (user_id, moderator_id, reason) VALUES ($1,$1,'profile visibility fixture')", actor.ID); err != nil {
		t.Fatal(err)
	}
	guest.call("GET", public, nil, 404)
	if avatar := guest.call("GET", public+"/avatar", nil, 200); avatar.Header().Get("Content-Type") != "image/svg+xml" {
		t.Fatal("restricted avatar leaked")
	}
	owner.call("GET", "/account/profile/avatar", nil, 200)
	owner.post("/account/profile", url.Values{"bio": {"Edited"}, "remove_image": {"1"}}, 303)
	if avatar := owner.call("GET", "/account/profile/avatar", nil, 200); avatar.Header().Get("Content-Type") != "image/svg+xml" {
		t.Fatal("avatar removal failed")
	}
}
