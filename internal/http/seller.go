package web

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/x4342/marketplace/internal/db"
	"github.com/x4342/marketplace/internal/media"
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
	mux.HandleFunc("POST /sell/new/image-preview", p.imagePreview)
	mux.HandleFunc("POST /sell/drafts/{id}/image-preview", p.imagePreview)
	mux.HandleFunc("GET /sell/listings/{id}/image", p.ownerListingImage)
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
	p.render(w, r, http.StatusOK, templates.SellerDashboard(csrfFor(w, r), r.URL.Query().Get("updated") == "1", away, drafts, listings))
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
	p.render(w, r, http.StatusOK, templates.DraftPreview(form))
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
	if kind, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); kind == "multipart/form-data" {
		release, ok := media.TryAcquire()
		if !ok {
			http.Error(w, "Another image is being processed. Please try again.", http.StatusServiceUnavailable)
			return
		}
		defer release()
	}
	rawImage, uploadErr := readListingSubmission(w, r)
	if !validCSRF(r) {
		p.denied(w, r)
		return
	}
	form.Fields = seller.FromForm(r.PostForm)
	form.Errors = form.Fields.SizeErrors()
	var image []byte
	if uploadErr != nil {
		form.Errors = append(form.Errors, uploadErr.Error())
	} else if len(rawImage) > 0 {
		var crop media.Crop
		crop, uploadErr = media.ParseCrop(r.PostForm.Get("crop"))
		if len(r.PostForm["crop"]) > 1 {
			uploadErr = errors.New("Submit only one crop selection.")
		}
		if uploadErr == nil {
			var result media.Result
			result, uploadErr = media.Square(rawImage, crop)
			image = result.JPEG
		}
		if uploadErr != nil {
			form.Errors = append(form.Errors, uploadErr.Error())
		}
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
	if r.Header.Get("X-Draft-Upload") == "1" {
		w.Header().Set("X-Draft-Location", destination)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, destination, http.StatusSeeOther)
}

// imagePreview returns a normalized frame without saving the source or changing the draft.
func (p sellerPages) imagePreview(w http.ResponseWriter, r *http.Request) {
	session, ok := p.requireSession(w, r)
	if !ok {
		return
	}
	if r.PathValue("id") != "" {
		_, err := p.queries.SellerDraft(r.Context(), db.SellerDraftParams{ID: listingID(r), SellerID: session.ID})
		if err != nil {
			p.failure(w, r, err)
			return
		}
	}
	release, ok := media.TryAcquire()
	if !ok {
		http.Error(w, "Another image is being processed. Please try again.", http.StatusServiceUnavailable)
		return
	}
	defer release()
	raw, err := readListingSubmission(w, r)
	if !validCSRF(r) {
		p.denied(w, r)
		return
	}
	if err == nil {
		var preview media.Result
		if selection := r.PostForm.Get("crop"); selection != "" {
			var crop media.Crop
			crop, err = media.ParseCrop(selection)
			if err == nil && len(r.PostForm["crop"]) == 1 {
				preview, err = media.Square(raw, crop)
			} else if err == nil {
				err = errors.New("Submit only one crop selection.")
			}
		} else {
			preview, err = media.Frame(raw)
		}
		if err == nil {
			w.Header().Set("X-Image-Width", strconv.Itoa(preview.Source.X))
			w.Header().Set("X-Image-Height", strconv.Itoa(preview.Source.Y))
			sendJPEG(w, preview.JPEG)
			return
		}
	}
	http.Error(w, err.Error(), http.StatusUnprocessableEntity)
}

func (p sellerPages) ownerListingImage(w http.ResponseWriter, r *http.Request) {
	session, ok := p.requireSession(w, r)
	if !ok {
		return
	}
	data, err := p.queries.OwnerListingImage(r.Context(), db.OwnerListingImageParams{ID: listingID(r), SellerID: session.ID})
	if err != nil {
		p.failure(w, r, err)
		return
	}
	sendJPEG(w, data)
}

// Read text fields preceding the upload so errors can redisplay unsaved edits.
func readListingSubmission(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	r.Body = http.MaxBytesReader(w, r.Body, media.MaxUpload+(64<<10))
	reader, err := r.MultipartReader()
	if errors.Is(err, http.ErrNotMultipart) {
		return nil, r.ParseForm()
	}
	if err != nil {
		return nil, errors.New("The listing form could not be read.")
	}
	r.PostForm = make(url.Values)
	var image []byte
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			return image, nil
		}
		if err != nil {
			return nil, errors.New("The form or image exceeded the upload limit.")
		}
		name := part.FormName()
		if name == "image" && part.FileName() != "" {
			if image != nil {
				return nil, errors.New("Upload one listing image at a time.")
			}
			image, err = io.ReadAll(io.LimitReader(part, media.MaxUpload+1))
			if err != nil || len(image) > media.MaxUpload {
				return nil, errors.New("Upload one JPEG, PNG, or WebP of 10 MB or less.")
			}
			if len(image) == 0 {
				return nil, errors.New("Choose a nonempty JPEG, PNG, or WebP.")
			}
			continue
		}
		value, err := io.ReadAll(io.LimitReader(part, (16<<10)+1))
		if err != nil || len(value) > 16<<10 {
			return nil, errors.New("One of the listing fields is too long.")
		}
		if name != "" && part.FileName() == "" {
			r.PostForm.Add(name, string(value))
		}
	}
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
	p.render(w, r, status, templates.SellerDraftForm(form))
}

func (p sellerPages) failure(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		p.render(w, r, http.StatusNotFound, templates.Problem("Listing unavailable", "Return to Sell to find your editable listings."))
	case errors.Is(err, seller.ErrConflict):
		p.render(w, r, http.StatusConflict, templates.Problem("Listing changed", "Open the latest draft from Sell and retry your changes."))
	default:
		p.log.Error("seller listing operation failed")
		p.render(w, r, http.StatusServiceUnavailable, templates.Problem("Seller tools unavailable", "Try again shortly."))
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
