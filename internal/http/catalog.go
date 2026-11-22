package web

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/x4342/marketplace/internal/catalog"
	"github.com/x4342/marketplace/internal/db"
	"github.com/x4342/marketplace/internal/identity"
	"github.com/x4342/marketplace/web/templates"
)

type catalogPages struct {
	identityPages
	queries *db.Queries
}

func (a app) mountCatalog(mux *http.ServeMux, queries *db.Queries, accounts *identity.Service) {
	pages := catalogPages{identityPages: identityPages{app: a, accounts: accounts}, queries: queries}
	mux.HandleFunc("GET /discover", pages.discover)
	mux.HandleFunc("GET /listings/{id}", pages.listing)
	mux.HandleFunc("GET /listings/{id}/image", pages.image)
	mux.HandleFunc("POST /listings/{id}/favorite", pages.favorite)
	mux.HandleFunc("GET /stores/{username}", pages.storefront)
}

func (p catalogPages) discover(w http.ResponseWriter, r *http.Request) {
	p.renderCatalog(w, r, r.URL.Query(), "")
}

func (p catalogPages) storefront(w http.ResponseWriter, r *http.Request) {
	store, err := p.queries.PublicStorefront(r.Context(), r.PathValue("username"))
	if err != nil {
		p.catalogError(w, r, err)
		return
	}
	params := r.URL.Query()
	params.Set("seller", store.Username)
	p.renderCatalog(w, r, params, store.Username)
}

func (p catalogPages) renderCatalog(w http.ResponseWriter, r *http.Request, values url.Values, storefront string) {
	categories, err := p.queries.CatalogCategories(r.Context())
	if err != nil {
		p.catalogError(w, r, err)
		return
	}
	filters := catalog.Parse(values, categories)
	session, err := p.accounts.Active(r.Context(), cookieValue(r, sessionCookie))
	if err != nil && !errors.Is(err, identity.ErrInvalid) {
		p.catalogError(w, r, err)
		return
	}
	var csrf string
	if err == nil {
		filters.Query.ViewerID = session.ID
		csrf = csrfFor(w, r)
	} else if filters.Query.SavedOnly {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	rows, err := p.queries.DiscoverListings(r.Context(), filters.Query)
	if err != nil {
		p.catalogError(w, r, err)
		return
	}
	rate, err := p.queries.CatalogFX(r.Context())
	if err != nil {
		p.catalogError(w, r, err)
		return
	}
	results := templates.DiscoveryData{Filters: filters, Categories: categories, Listings: rows, Storefront: storefront, CSRF: csrf, ReturnURL: filters.Link(filters.Page), RateDate: rate.RateDate.Time.Format("2 January 2006")}
	if len(rows) > 0 {
		results.Total = rows[0].TotalCount
	}
	if filters.Page > 1 {
		results.Previous = filters.Link(filters.Page - 1)
	}
	if int64(filters.Page*catalog.PageSize) < results.Total {
		results.Next = filters.Link(filters.Page + 1)
	}
	p.render(w, r, http.StatusOK, templates.Discover(theme(r), results))
}

func (p catalogPages) listing(w http.ResponseWriter, r *http.Request) {
	var id pgtype.UUID
	if err := id.Scan(r.PathValue("id")); err != nil {
		p.catalogError(w, r, pgx.ErrNoRows)
		return
	}
	listing, err := p.queries.PublicListing(r.Context(), id)
	if err != nil {
		p.catalogError(w, r, err)
		return
	}
	session, err := p.accounts.Active(r.Context(), cookieValue(r, sessionCookie))
	var csrf string
	var favorite bool
	if err == nil {
		csrf = csrfFor(w, r)
		favorite, err = p.queries.HasFavorite(r.Context(), db.HasFavoriteParams{UserID: session.ID, ListingID: id})
	}
	if err != nil && !errors.Is(err, identity.ErrInvalid) {
		p.catalogError(w, r, err)
		return
	}
	p.render(w, r, http.StatusOK, templates.ListingDetail(theme(r), listing, strings.Join(listing.ShipRegions, ", "), csrf, favorite))
}

func (p catalogPages) image(w http.ResponseWriter, r *http.Request) {
	content, err := p.queries.PublicListingImage(r.Context(), listingID(r))
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		p.catalogError(w, r, err)
		return
	}
	sendJPEG(w, content)
}

func (p catalogPages) favorite(w http.ResponseWriter, r *http.Request) {
	if !validForm(w, r) {
		p.denied(w, r)
		return
	}
	session, ok := p.requireSession(w, r)
	if !ok {
		return
	}
	var id pgtype.UUID
	if err := id.Scan(r.PathValue("id")); err != nil {
		p.catalogError(w, r, pgx.ErrNoRows)
		return
	}
	if _, err := p.queries.PublicListing(r.Context(), id); err != nil {
		p.catalogError(w, r, err)
		return
	}
	var err error
	switch r.PostForm.Get("favorite") {
	case "save":
		err = p.queries.SaveFavorite(r.Context(), db.SaveFavoriteParams{UserID: session.ID, ID: id})
	case "remove":
		err = p.queries.RemoveFavorite(r.Context(), db.RemoveFavoriteParams{UserID: session.ID, ListingID: id})
	default:
		p.render(w, r, http.StatusBadRequest, templates.Problem(theme(r), "Invalid favorites request", "Use the controls on the listing page."))
		return
	}
	if err != nil {
		p.catalogError(w, r, err)
		return
	}
	destination := "/listings/" + id.String()
	if next, err := url.ParseRequestURI(r.PostForm.Get("return")); err == nil &&
		next.Scheme == "" && next.Host == "" && next.Path == "/discover" {
		destination = "/discover"
		if query := next.Query().Encode(); query != "" {
			destination += "?" + query
		}
	}
	http.Redirect(w, r, destination, http.StatusSeeOther)
}

func (p catalogPages) catalogError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		p.render(w, r, http.StatusNotFound, templates.Problem(theme(r), "Page unavailable", "Return to discovery to find published listings and storefronts."))
		return
	}
	p.log.Error("catalog query failed")
	p.render(w, r, http.StatusServiceUnavailable, templates.Problem(theme(r), "Catalog unavailable", "Try again shortly."))
}
