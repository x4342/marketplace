package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/x4342/marketplace/internal/db"
	"github.com/x4342/marketplace/internal/seller"
	"github.com/x4342/marketplace/web/templates"
)

type sellerPages struct {
	identityPages
	queries *db.Queries
	service seller.Service
}

func (p sellerPages) mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /sell", p.dashboard)
	mux.HandleFunc("GET /sell/new", p.newListing)
	mux.HandleFunc("POST /sell/new", p.save)
	mux.HandleFunc("GET /sell/drafts/{id}", p.editDraft)
	mux.HandleFunc("POST /sell/drafts/{id}", p.save)
	mux.HandleFunc("GET /sell/drafts/{id}/preview", p.preview)
	mux.HandleFunc("POST /sell/drafts/{id}/publish", p.publish)
	mux.HandleFunc("GET /sell/drafts/{id}/image", p.draftImage)
	mux.HandleFunc("POST /sell/listings/{id}/edit", p.openDraft)
	mux.HandleFunc("POST /sell/listings/{id}/status", p.status)
	mux.HandleFunc("POST /sell/vacation", p.vacation)
}

func (p sellerPages) dashboard(w http.ResponseWriter, r *http.Request) {
	session, ok := p.requireSession(w, r)
	if !ok {
		return
	}
	drafts, err := p.queries.SellerDrafts(r.Context(), session.ID)
	if err != nil {
		p.failure(w, r, err)
		return
	}
	listings, err := p.queries.SellerListings(r.Context(), session.ID)
	if err != nil {
		p.failure(w, r, err)
		return
	}
	away, err := p.queries.SellerVacation(r.Context(), session.ID)
	if err != nil {
		p.failure(w, r, err)
		return
	}
	p.render(w, r, http.StatusOK, templates.SellerDashboard(theme(r), csrfFor(w, r), r.URL.Query().Get("updated") == "1", away, drafts, listings))
}

func (p sellerPages) newListing(w http.ResponseWriter, r *http.Request) {
	if _, ok := p.requireSession(w, r); ok {
		p.form(w, r, http.StatusOK, templates.DraftForm{Fields: seller.Fields{Currency: "USD", Stock: "0"}})
	}
}

func (p sellerPages) draft(w http.ResponseWriter, r *http.Request) (templates.DraftForm, bool) {
	session, ok := p.requireSession(w, r)
	if !ok {
		return templates.DraftForm{}, false
	}
	id := listingID(r)
	row, err := p.queries.SellerDraft(r.Context(), db.SellerDraftParams{ID: id, SellerID: session.ID})
	if err != nil {
		p.failure(w, r, err)
		return templates.DraftForm{}, false
	}
	form := templates.DraftForm{ID: id.String(), Revision: row.Revision, HasImage: row.HasImage}
	if err := json.Unmarshal(row.Fields, &form.Fields); err != nil {
		p.failure(w, r, err)
		return templates.DraftForm{}, false
	}
	return form, true
}

func (p sellerPages) editDraft(w http.ResponseWriter, r *http.Request) {
	if form, ok := p.draft(w, r); ok {
		form.Saved = r.URL.Query().Get("saved") == "1"
		p.form(w, r, http.StatusOK, form)
	}
}

func (p sellerPages) preview(w http.ResponseWriter, r *http.Request) {
	form, ok := p.draft(w, r)
	if !ok {
		return
	}
	categories, err := p.queries.CatalogCategories(r.Context())
	if err != nil {
		p.failure(w, r, err)
		return
	}
	form.CSRF = csrfFor(w, r)
	_, form.Errors = form.Fields.Publication(pgtype.UUID{}, pgtype.UUID{}, categories)
	p.render(w, r, http.StatusOK, templates.DraftPreview(theme(r), form))
}

func (p sellerPages) save(w http.ResponseWriter, r *http.Request) {
	session, ok := p.requireSession(w, r)
	if !ok {
		return
	}
	var form templates.DraftForm
	id := listingID(r)
	if r.PathValue("id") != "" {
		row, err := p.queries.SellerDraft(r.Context(), db.SellerDraftParams{ID: id, SellerID: session.ID})
		if err != nil {
			p.failure(w, r, err)
			return
		}
		form.ID, form.HasImage, form.Revision = id.String(), row.HasImage, row.Revision
	}
	r.Body = http.MaxBytesReader(w, r.Body, seller.MaxUpload+(64<<10))
	parseErr := r.ParseMultipartForm(seller.MaxUpload + (64 << 10))
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	if parseErr != nil && !errors.Is(parseErr, http.ErrNotMultipart) {
		form.Errors = []string{"The form or image exceeds the upload limit. Use a JPEG or PNG of 3 MB or less."}
		if form.ID != "" {
			http.Redirect(w, r, "/sell/drafts/"+form.ID+"?upload=too-large", http.StatusSeeOther)
			return
		}
		p.form(w, r, http.StatusBadRequest, form)
		return
	}
	if !validCSRF(r) {
		p.denied(w, r)
		return
	}
	form.Fields = seller.FromForm(r.PostForm)
	form.Errors = form.Fields.SizeErrors()
	var image []byte
	file, _, err := r.FormFile("image")
	if err == nil {
		defer file.Close()
		image, err = seller.ReadImage(file)
		if err != nil {
			form.Errors = append(form.Errors, err.Error())
		}
	} else if !errors.Is(err, http.ErrMissingFile) && !errors.Is(err, http.ErrNotMultipart) {
		form.Errors = append(form.Errors, "The image could not be read.")
	}
	removeImage := r.PostForm.Get("remove_image") == "1"
	if removeImage && len(image) > 0 {
		form.Errors = append(form.Errors, "Choose either a replacement image or remove the current image.")
	}
	if len(form.Errors) > 0 {
		p.form(w, r, http.StatusUnprocessableEntity, form)
		return
	}
	payload, err := json.Marshal(form.Fields)
	if err != nil {
		p.failure(w, r, err)
		return
	}
	if form.ID == "" {
		id, err = p.queries.CreateListingDraft(r.Context(), db.CreateListingDraftParams{SellerID: session.ID, Fields: payload, ImageJpeg: image})
	} else {
		var changed int64
		changed, err = p.queries.UpdateListingDraft(r.Context(), db.UpdateListingDraftParams{
			ID: id, SellerID: session.ID, Revision: postedRevision(r), Fields: payload, ImageJpeg: image, RemoveImage: removeImage,
		})
		if err == nil && changed == 0 {
			err = seller.ErrConflict
		}
	}
	if err != nil {
		p.failure(w, r, err)
		return
	}
	destination := "/sell/drafts/" + id.String()
	if r.PostForm.Get("action") == "preview" {
		destination += "/preview"
	} else {
		destination += "?saved=1"
	}
	http.Redirect(w, r, destination, http.StatusSeeOther)
}

func (p sellerPages) publish(w http.ResponseWriter, r *http.Request) {
	session, ok := p.authorizeMutation(w, r)
	if !ok {
		return
	}
	id := listingID(r)
	problems, err := p.service.Publish(r.Context(), id, session.ID, postedRevision(r))
	if err != nil {
		p.failure(w, r, err)
		return
	}
	if len(problems) != 0 {
		http.Redirect(w, r, "/sell/drafts/"+id.String()+"/preview", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/listings/"+id.String(), http.StatusSeeOther)
}

func (p sellerPages) openDraft(w http.ResponseWriter, r *http.Request) {
	session, ok := p.authorizeMutation(w, r)
	if !ok {
		return
	}
	id := listingID(r)
	_, err := p.queries.OpenListingDraft(r.Context(), db.OpenListingDraftParams{ID: id, SellerID: session.ID})
	if err != nil {
		p.failure(w, r, err)
		return
	}
	if _, err := p.queries.SellerDraft(r.Context(), db.SellerDraftParams{ID: id, SellerID: session.ID}); err != nil {
		p.failure(w, r, err)
		return
	}
	http.Redirect(w, r, "/sell/drafts/"+id.String(), http.StatusSeeOther)
}

func (p sellerPages) status(w http.ResponseWriter, r *http.Request) {
	session, ok := p.authorizeMutation(w, r)
	if !ok {
		return
	}
	count, err := p.queries.ChangeSellerListingStatus(r.Context(), db.ChangeSellerListingStatusParams{
		ID: listingID(r), SellerID: session.ID, NextStatus: r.PostForm.Get("status"),
	})
	if err != nil {
		p.failure(w, r, err)
	} else if count == 0 {
		p.failure(w, r, pgx.ErrNoRows)
	} else {
		http.Redirect(w, r, "/sell?updated=1", http.StatusSeeOther)
	}
}

func (p sellerPages) vacation(w http.ResponseWriter, r *http.Request) {
	session, ok := p.authorizeMutation(w, r)
	if !ok {
		return
	}
	var err error
	switch r.PostForm.Get("vacation") {
	case "start":
		err = p.queries.StartSellerVacation(r.Context(), session.ID)
	case "end":
		err = p.queries.EndSellerVacation(r.Context(), session.ID)
	default:
		p.denied(w, r)
		return
	}
	if err != nil {
		p.failure(w, r, err)
		return
	}
	http.Redirect(w, r, "/sell?updated=1", http.StatusSeeOther)
}

func (p sellerPages) draftImage(w http.ResponseWriter, r *http.Request) {
	session, ok := p.requireSession(w, r)
	if !ok {
		return
	}
	jpeg, err := p.queries.PrivateDraftImage(r.Context(), db.PrivateDraftImageParams{ID: listingID(r), SellerID: session.ID})
	if err != nil {
		p.failure(w, r, err)
		return
	}
	sendJPEG(w, jpeg)
}

func (p sellerPages) authorizeMutation(w http.ResponseWriter, r *http.Request) (db.ActiveSessionRow, bool) {
	if !validForm(w, r) {
		p.denied(w, r)
		return db.ActiveSessionRow{}, false
	}
	return p.requireSession(w, r)
}

func (p sellerPages) form(w http.ResponseWriter, r *http.Request, status int, form templates.DraftForm) {
	categories, err := p.queries.CatalogCategories(r.Context())
	if err != nil {
		p.failure(w, r, err)
		return
	}
	form.Categories, form.CSRF = categories, csrfFor(w, r)
	if r.URL.Query().Get("upload") == "too-large" {
		form.Errors = append(form.Errors, "The image exceeded the upload limit. Your prior draft is still saved.")
	}
	p.render(w, r, status, templates.SellerDraftForm(theme(r), form))
}

func (p sellerPages) failure(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		p.render(w, r, http.StatusNotFound, templates.Problem(theme(r), "Listing unavailable", "Return to Sell to find your editable listings."))
	case errors.Is(err, seller.ErrConflict):
		p.render(w, r, http.StatusConflict, templates.Problem(theme(r), "Listing changed", "Open the latest draft from Sell and retry your changes."))
	default:
		p.log.Error("seller listing operation failed")
		p.render(w, r, http.StatusServiceUnavailable, templates.Problem(theme(r), "Seller tools unavailable", "Try again shortly."))
	}
}

func listingID(r *http.Request) pgtype.UUID {
	var id pgtype.UUID
	_ = id.Scan(r.PathValue("id"))
	return id
}

func postedRevision(r *http.Request) int64 {
	revision, _ := strconv.ParseInt(r.PostForm.Get("revision"), 10, 64)
	return revision
}

func sendJPEG(w http.ResponseWriter, content []byte) {
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Length", strconv.Itoa(len(content)))
	_, _ = w.Write(content)
}
