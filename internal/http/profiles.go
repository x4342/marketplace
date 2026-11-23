package web

import (
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/x4342/marketplace/internal/db"
	"github.com/x4342/marketplace/internal/media"
	"github.com/x4342/marketplace/web/templates"
)

type profilePages struct {
	identityPages
	queries *db.Queries
}

func (p profilePages) mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /users/{username}", p.public)
	mux.HandleFunc("GET /users/{username}/avatar", p.avatar)
	mux.HandleFunc("GET /account/profile", p.edit)
	mux.HandleFunc("POST /account/profile", p.save)
	mux.HandleFunc("GET /account/profile/avatar", p.privateAvatar)
	mux.HandleFunc("POST /account/profile/image-preview", p.preview)
}

func (p profilePages) public(w http.ResponseWriter, r *http.Request) {
	profile, err := p.queries.PublicProfile(r.Context(), r.PathValue("username"))
	if err != nil {
		p.profileError(w, r, err)
		return
	}
	page := inboxPage(r)
	listings, err := p.queries.ProfileListings(r.Context(), db.ProfileListingsParams{SellerID: profile.ID, PageOffset: 24 * (page - 1)})
	if err != nil {
		p.profileError(w, r, err)
		return
	}
	rating, err := p.queries.SellerRating(r.Context(), profile.ID)
	if err != nil {
		p.profileError(w, r, err)
		return
	}
	more := len(listings) > 24
	if more {
		listings = listings[:24]
	}
	p.render(w, r, 200, templates.PublicProfile(profile, listings, rating, page, more))
}

func (p profilePages) avatar(w http.ResponseWriter, r *http.Request) {
	jpeg, err := p.queries.PublicAvatar(r.Context(), r.PathValue("username"))
	p.sendAvatar(w, r, jpeg, err)
}

func (p profilePages) privateAvatar(w http.ResponseWriter, r *http.Request) {
	s, ok := p.requireSession(w, r)
	if !ok {
		return
	}
	jpeg, err := p.queries.OwnAvatar(r.Context(), s.ID)
	p.sendAvatar(w, r, jpeg, err)
}

func (p profilePages) sendAvatar(w http.ResponseWriter, r *http.Request, jpeg []byte, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		w.Header().Set("Content-Type", "image/svg+xml")
		_, _ = w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 256 256"><rect width="256" height="256" fill="#333940"/><g fill="#b6bcc2"><circle cx="128" cy="92" r="36"/><path d="M59 204a69 61 0 0 1 138 0Z"/></g></svg>`))
		return
	}
	if err != nil {
		p.profileError(w, r, err)
		return
	}
	sendJPEG(w, jpeg)
}

func (p profilePages) edit(w http.ResponseWriter, r *http.Request) {
	s, ok := p.requireSession(w, r)
	if !ok {
		return
	}
	row, err := p.queries.OwnProfile(r.Context(), s.ID)
	if err != nil {
		p.profileError(w, r, err)
		return
	}
	p.render(w, r, 200, templates.EditProfile(row.Bio, row.HasAvatar, csrfFor(w, r), r.URL.Query().Get("saved") == "1", nil))
}

func (p profilePages) preview(w http.ResponseWriter, r *http.Request) {
	if _, ok := p.requireSession(w, r); !ok {
		return
	}
	p.renderImagePreview(w, r, media.Avatar)
}

func (p profilePages) save(w http.ResponseWriter, r *http.Request) {
	s, ok := p.requireSession(w, r)
	if !ok {
		return
	}
	release, ok := media.TryAcquire()
	if !ok {
		http.Error(w, "Another image is being processed. Please try again.", 503)
		return
	}
	defer release()
	raw, err := readImageSubmission(w, r)
	if !validCSRF(r) {
		p.denied(w, r)
		return
	}
	bio := strings.TrimSpace(r.PostForm.Get("bio"))
	remove := r.PostForm.Get("remove_image") == "1"
	var problems []string
	var image []byte
	if !utf8.ValidString(bio) || strings.ContainsRune(bio, 0) || utf8.RuneCountInString(bio) > 500 {
		problems = append(problems, "Enter a biography of at most 500 characters.")
	}
	switch {
	case err != nil:
		problems = append(problems, err.Error())
	case len(raw) > 0 && remove:
		problems = append(problems, "Choose a new avatar or remove the current one.")
	case len(raw) > 0:
		crop, cropErr := media.ParseCrop(r.PostForm.Get("crop"))
		if len(r.PostForm["crop"]) > 1 {
			cropErr = errors.New("Submit only one crop selection.")
		}
		if cropErr == nil {
			var result media.Result
			result, cropErr = media.Avatar(raw, crop)
			image = result.JPEG
		}
		if cropErr != nil {
			problems = append(problems, cropErr.Error())
		}
	}
	if len(problems) > 0 {
		row, err := p.queries.OwnProfile(r.Context(), s.ID)
		if err != nil {
			p.profileError(w, r, err)
			return
		}
		p.render(w, r, 422, templates.EditProfile(bio, row.HasAvatar, csrfFor(w, r), false, problems))
		return
	}
	if err := p.queries.SaveProfile(r.Context(), db.SaveProfileParams{UserID: s.ID, Bio: bio, AvatarJpeg: image, RemoveAvatar: remove}); err != nil {
		p.profileError(w, r, err)
		return
	}
	const destination = "/account/profile?saved=1"
	if r.Header.Get("X-Draft-Upload") == "1" {
		w.Header().Set("X-Draft-Location", destination)
		w.WriteHeader(204)
		return
	}
	http.Redirect(w, r, destination, 303)
}

func (p profilePages) profileError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		p.render(w, r, 404, templates.Problem("Profile unavailable", "This profile is unavailable."))
		return
	}
	p.log.Error("profile operation failed")
	p.render(w, r, 503, templates.Problem("Profile unavailable", "Try again shortly."))
}
