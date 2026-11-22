package web

import (
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/x4342/marketplace/internal/db"
	"github.com/x4342/marketplace/internal/identity"
	"github.com/x4342/marketplace/internal/orders"
	"github.com/x4342/marketplace/web/templates"
)

type orderPages struct {
	identityPages
	queries *db.Queries
	service orders.Service
}

func (p orderPages) mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /orders", p.index)
	mux.HandleFunc("GET /orders/{id}", p.detail)
	mux.HandleFunc("POST /orders/{id}/actions", p.action)
	mux.HandleFunc("GET /orders/{id}/download", p.download)
	mux.HandleFunc("GET /listings/{id}/checkout", p.checkout)
	mux.HandleFunc("POST /listings/{id}/checkout", p.create)
	mux.HandleFunc("GET /sell/listings/{id}/download", p.asset)
	mux.HandleFunc("POST /sell/listings/{id}/download", p.storeAsset)
}

func (p orderPages) index(w http.ResponseWriter, r *http.Request) {
	session, ok := p.requireSession(w, r)
	if !ok {
		return
	}
	page := inboxPage(r)
	rows, err := p.queries.UserOrders(r.Context(), db.UserOrdersParams{Actor: session.ID, PageOffset: 30 * (page - 1)})
	if err != nil {
		p.failure(w, r, err)
		return
	}
	hasMore := len(rows) > 30
	if hasMore {
		rows = rows[:30]
	}
	p.render(w, r, http.StatusOK, templates.OrdersIndex(theme(r), session.ID, rows, page, hasMore))
}

func (p orderPages) checkout(w http.ResponseWriter, r *http.Request) {
	session, ok := p.requireSession(w, r)
	if !ok {
		return
	}
	listing, err := p.queries.PublicListing(r.Context(), listingID(r))
	if err != nil {
		p.failure(w, r, err)
		return
	}
	if listing.SellerID == session.ID || listing.SellerAway || (listing.Stock == 0 && !listing.CanBackorder) ||
		(listing.DeliveryMode == "automatic" && !listing.DigitalAssetID.Valid) {
		p.failure(w, r, orders.ErrUnavailable)
		return
	}
	p.render(w, r, http.StatusOK, templates.CheckoutForm(theme(r), listing, csrfFor(w, r), identity.NewToken(), "", "", ""))
}

func (p orderPages) create(w http.ResponseWriter, r *http.Request) {
	session, ok := p.authorized(w, r, 8<<10)
	if !ok {
		return
	}
	key := r.PostForm.Get("key")
	id := listingID(r)
	if !identity.ValidToken(key) || len(r.PostForm["key"]) != 1 {
		p.denied(w, r)
		return
	}
	orderID, err := p.service.Create(r.Context(), orders.Checkout{
		Buyer: session.ID, Listing: id, Key: key,
		Region: r.PostForm.Get("region"), Address: strings.TrimSpace(r.PostForm.Get("address")),
	})
	if errors.Is(err, orders.ErrShipping) {
		listing, queryErr := p.queries.PublicListing(r.Context(), id)
		if queryErr != nil {
			p.failure(w, r, queryErr)
			return
		}
		p.render(w, r, http.StatusUnprocessableEntity, templates.CheckoutForm(theme(r), listing, csrfFor(w, r), key,
			r.PostForm.Get("region"), r.PostForm.Get("address"), "Choose a supported region and enter a test shipping address up to 300 characters."))
		return
	}
	if err != nil && !orderID.Valid {
		p.failure(w, r, err)
		return
	}
	http.Redirect(w, r, orderPath(orderID), http.StatusSeeOther)
}

func (p orderPages) detail(w http.ResponseWriter, r *http.Request) {
	session, ok := p.requireSession(w, r)
	if !ok {
		return
	}
	id := listingID(r)
	row, err := p.queries.ParticipantOrder(r.Context(), db.ParticipantOrderParams{OrderID: id, Actor: session.ID})
	if err != nil {
		p.failure(w, r, err)
		return
	}
	timeline, err := p.queries.OrderTimeline(r.Context(), db.OrderTimelineParams{OrderID: id, Viewer: session.ID})
	if err != nil {
		p.failure(w, r, err)
		return
	}
	data := templates.OrderData{Order: row, Buyer: row.BuyerID == session.ID, Timeline: timeline, CSRF: csrfFor(w, r)}
	data.PurchaseThread, err = p.queries.OrderPurchaseConversation(r.Context(), db.OrderPurchaseConversationParams{OrderID: id, Actor: session.ID})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		p.failure(w, r, err)
		return
	}
	if data.Buyer && row.State == "awaiting_payment" {
		data.Quote, err = p.queries.OrderQuote(r.Context(), id)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			p.failure(w, r, err)
			return
		}
	}
	if row.Kind == "physical" {
		data.Shipping, err = p.queries.OrderShipping(r.Context(), db.OrderShippingParams{OrderID: id, Actor: session.ID})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			p.failure(w, r, err)
			return
		}
		data.Tracking, err = p.queries.OrderShipment(r.Context(), db.OrderShipmentParams{OrderID: id, Actor: session.ID})
		if err == nil {
			data.HasShipment = true
		} else if !errors.Is(err, pgx.ErrNoRows) {
			p.failure(w, r, err)
			return
		}
	}
	if row.DeliveryMode == "manual" {
		data.Manual, err = p.queries.ManualTask(r.Context(), db.ManualTaskParams{OrderID: id, Actor: session.ID})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			p.failure(w, r, err)
			return
		}
	}
	if data.Buyer {
		data.Review, err = p.queries.BuyerReviewStatus(r.Context(), db.BuyerReviewStatusParams{Actor: session.ID, OrderID: id})
		if err != nil {
			p.failure(w, r, err)
			return
		}
		data.ReviewSaved = r.URL.Query().Get("review") == "saved"
	}

	p.render(w, r, http.StatusOK, templates.OrderPage(theme(r), data))
}

func (p orderPages) action(w http.ResponseWriter, r *http.Request) {
	session, ok := p.authorized(w, r, 32<<10)
	if !ok {
		return
	}
	id := listingID(r)
	var err error
	switch action := r.PostForm.Get("action"); action {
	case "quote":
		err = p.service.EnsureQuote(r.Context(), id, session.ID)
	case "simulate":
		err = p.service.SimulatePayment(r.Context(), id, session.ID)
	case "cancel":
		err = p.service.Cancel(r.Context(), id, session.ID)
	default:
		err = p.service.Act(r.Context(), id, session.ID, action, strings.TrimSpace(r.PostForm.Get("details")))
	}
	if err != nil {
		p.failure(w, r, err)
		return
	}
	http.Redirect(w, r, orderPath(id), http.StatusSeeOther)
}

func (p orderPages) download(w http.ResponseWriter, r *http.Request) {
	session, ok := p.requireSession(w, r)
	if !ok {
		return
	}
	payload, err := p.queries.BuyerDownload(r.Context(), db.BuyerDownloadParams{OrderID: listingID(r), Buyer: session.ID})
	if err != nil {
		p.failure(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="marketplace-delivery.txt"`)
	_, _ = w.Write([]byte(payload))
}

func (p orderPages) asset(w http.ResponseWriter, r *http.Request) {
	session, ok := p.requireSession(w, r)
	if !ok {
		return
	}
	id := listingID(r)
	title, err := p.queries.OwnedAutomaticListing(r.Context(), db.OwnedAutomaticListingParams{ID: id, SellerID: session.ID})
	if err != nil {
		p.failure(w, r, err)
		return
	}
	p.render(w, r, http.StatusOK, templates.AssetForm(theme(r), id.String(), title, csrfFor(w, r), "", r.URL.Query().Get("saved") == "1"))
}

func (p orderPages) storeAsset(w http.ResponseWriter, r *http.Request) {
	session, ok := p.authorized(w, r, (3*65536)+(8<<10))
	if !ok {
		return
	}
	id := listingID(r)
	title, err := p.queries.OwnedAutomaticListing(r.Context(), db.OwnedAutomaticListingParams{ID: id, SellerID: session.ID})
	if err != nil {
		p.failure(w, r, err)
		return
	}
	if err := p.service.SetAsset(r.Context(), id, session.ID, r.PostForm.Get("payload")); err != nil {
		if errors.Is(err, orders.ErrState) {
			p.render(w, r, http.StatusUnprocessableEntity, templates.AssetForm(theme(r), id.String(), title, csrfFor(w, r), "Enter a nonempty UTF-8 test document up to 64 KiB.", false))
			return
		}
		p.failure(w, r, err)
		return
	}
	http.Redirect(w, r, "/sell/listings/"+id.String()+"/download?saved=1", http.StatusSeeOther)
}

func (p orderPages) authorized(w http.ResponseWriter, r *http.Request, limit int64) (db.ActiveSessionRow, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	if err := r.ParseForm(); err != nil || !validCSRF(r) {
		p.denied(w, r)
		return db.ActiveSessionRow{}, false
	}
	return p.requireSession(w, r)
}

func (p orderPages) failure(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		p.render(w, r, http.StatusNotFound, templates.OrderProblem(theme(r), "Order resource unavailable", "Check the listing or open your orders."))
	case errors.Is(err, orders.ErrUnavailable):
		p.render(w, r, http.StatusConflict, templates.OrderProblem(theme(r), "Listing unavailable for checkout", "Return to discovery and choose an available listing."))
	case errors.Is(err, orders.ErrState):
		p.render(w, r, http.StatusConflict, templates.OrderProblem(theme(r), "Order action unavailable", "Check the current order state, quote expiry, and required details."))
	case errors.Is(err, orders.ErrQuote):
		p.render(w, r, http.StatusUnprocessableEntity, templates.OrderProblem(theme(r), "Simulated payment rejected", "The simulated payment did not match the saved quote."))
	default:
		p.log.Error("order operation failed")
		p.render(w, r, http.StatusServiceUnavailable, templates.OrderProblem(theme(r), "Orders unavailable", "Try again shortly."))
	}
}

func orderPath(id pgtype.UUID) string { return "/orders/" + id.String() }
