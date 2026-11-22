package orders

import (
	"context"
	"crypto/sha256"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/x4342/marketplace/internal/db"
)

var (
	ErrUnavailable = errors.New("listing unavailable")
	ErrShipping    = errors.New("invalid shipping details")
	ErrState       = errors.New("order action unavailable")
	ErrQuote       = errors.New("payment does not match the simulated quote")
)

type Service struct {
	Pool          *pgxpool.Pool
	Provider      Provider
	QuoteLifetime time.Duration
	ManualWindow  time.Duration
}

func Simulation(pool *pgxpool.Pool) Service {
	return Service{Pool: pool, Provider: MockProvider{}, QuoteLifetime: 20 * time.Minute, ManualWindow: 48 * time.Hour}
}

type Checkout struct {
	Buyer   pgtype.UUID
	Listing pgtype.UUID
	Key     string
	Region  string
	Address string
}

func (s Service) Create(ctx context.Context, input Checkout) (pgtype.UUID, error) {
	digest := sha256.Sum256([]byte(input.Key))
	var orderID pgtype.UUID
	err := s.transaction(ctx, func(q *db.Queries) error {
		key := db.OrderByCheckoutKeyParams{BuyerID: input.Buyer, CheckoutKey: digest[:]}
		if found, err := q.OrderByCheckoutKey(ctx, key); err == nil {
			if found.ListingID != input.Listing {
				return ErrState
			}
			orderID = found.ID
			return nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		listing, err := q.CheckoutListing(ctx, input.Listing)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUnavailable
		}
		if err != nil {
			return err
		}
		// Read again after locking the listing so a concurrent retry sees its committed order.
		if found, err := q.OrderByCheckoutKey(ctx, key); err == nil {
			if found.ListingID != input.Listing {
				return ErrState
			}
			orderID = found.ID
			return nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		restricted, err := q.UserRestricted(ctx, input.Buyer)
		if err != nil {
			return err
		}
		if restricted {
			return ErrUnavailable
		}
		away, err := q.SellerVacation(ctx, listing.SellerID)
		if err != nil {
			return err
		}
		if away || listing.Status != "published" || listing.SellerID == input.Buyer ||
			(listing.Stock == 0 && !listing.CanBackorder) ||
			(listing.DeliveryMode == "automatic" && !listing.DigitalAssetID.Valid) {
			return ErrUnavailable
		}
		if listing.Kind == "physical" && (!slices.Contains(listing.ShipRegions, input.Region) || !validText(input.Address, 300)) {
			return ErrShipping
		}
		id, err := q.CreateUnpaidOrder(ctx, db.CreateUnpaidOrderParams{
			BuyerID: input.Buyer, SellerID: listing.SellerID, ListingID: input.Listing,
			CheckoutKey: digest[:], CheckoutDeadline: timestamp(time.Now().Add(s.QuoteLifetime)),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrState
		}
		if err != nil {
			return err
		}
		orderID = id
		var asset pgtype.UUID
		if listing.DeliveryMode == "automatic" {
			asset = listing.DigitalAssetID
		}
		reserved := listing.Stock > 0
		if err := q.SnapshotOrderItem(ctx, db.SnapshotOrderItemParams{
			OrderID: id, Title: listing.Title, Kind: listing.Kind, PriceMinor: listing.PriceMinor,
			Currency: listing.Currency, Terms: listing.Terms, DeliveryMode: listing.DeliveryMode,
			StockReserved: reserved, AssetID: asset,
		}); err != nil {
			return err
		}
		if reserved {
			if err := q.ReserveListingUnit(ctx, listing.ID); err != nil {
				return err
			}
		}
		if listing.Kind == "physical" {
			if err := q.StoreShippingAddress(ctx, db.StoreShippingAddressParams{OrderID: id, Region: input.Region, Address: input.Address}); err != nil {
				return err
			}
		}
		return record(ctx, q, id, input.Buyer, "awaiting_payment", "Order created before quote")
	})
	if err != nil {
		return pgtype.UUID{}, err
	}
	// The checkout and its snapshot must be committed before calling a provider.
	return orderID, s.EnsureQuote(ctx, orderID, input.Buyer)
}

func (s Service) EnsureQuote(ctx context.Context, id, buyer pgtype.UUID) error {
	q := db.New(s.Pool)
	order, err := q.ParticipantOrder(ctx, db.ParticipantOrderParams{OrderID: id, Actor: buyer})
	if err != nil {
		return err
	}
	if order.BuyerID != buyer {
		return pgx.ErrNoRows
	}
	if _, err := q.OrderQuote(ctx, id); !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if order.State != "awaiting_payment" || !time.Now().Before(order.CheckoutDeadline.Time) {
		return ErrState
	}
	quote, err := s.Provider.Quote(ctx, order.ID, order.PriceMinor, order.CheckoutDeadline.Time)
	if err != nil {
		return err
	}
	if quote.OrderID != id || !quote.ExpiresAt.Valid || !quote.ExpiresAt.Time.Equal(order.CheckoutDeadline.Time) {
		return ErrQuote
	}
	return s.transaction(ctx, func(q *db.Queries) error {
		row, err := locked(ctx, q, id, buyer)
		if err != nil {
			return err
		}
		if row.State != "awaiting_payment" || !time.Now().Before(row.CheckoutDeadline.Time) {
			return ErrState
		}
		return q.StoreQuote(ctx, quote)
	})
}

func (s Service) SimulatePayment(ctx context.Context, id, buyer pgtype.UUID) error {
	q := db.New(s.Pool)
	order, err := q.ParticipantOrder(ctx, db.ParticipantOrderParams{OrderID: id, Actor: buyer})
	if err != nil {
		return err
	}
	if order.BuyerID != buyer {
		return pgx.ErrNoRows
	}
	if order.State == "cancelled" || order.State == "expired" {
		return ErrState
	}
	quote, err := q.OrderQuote(ctx, id)
	if err != nil {
		return err
	}
	observation, err := s.Provider.Observe(ctx, quote)
	if err != nil {
		return err
	}
	return s.verify(ctx, id, buyer, observation)
}

func (s Service) verify(ctx context.Context, id, buyer pgtype.UUID, observation db.StorePaymentObservationParams) error {
	expired := false
	err := s.transaction(ctx, func(q *db.Queries) error {
		order, err := locked(ctx, q, id, buyer)
		if err != nil {
			return err
		}
		if order.BuyerID != buyer {
			return pgx.ErrNoRows
		}
		quote, err := q.OrderQuote(ctx, id)
		if err != nil {
			return err
		}
		if observation.EventKey == "" || observation.OrderID != id || observation.ProviderRef != quote.ProviderRef ||
			observation.Units != quote.Units || observation.Asset != quote.Asset || observation.Network != quote.Network {
			return ErrQuote
		}
		switch order.State {
		case "cancelled", "expired":
			return ErrState
		case "awaiting_payment":
			if !time.Now().Before(quote.ExpiresAt.Time) || !time.Now().Before(order.CheckoutDeadline.Time) {
				expired = true
				return release(ctx, q, order, pgtype.UUID{}, "expired")
			}
		default:
			return nil // Repeated verification cannot replay paid state.
		}
		inserted, err := q.StorePaymentObservation(ctx, observation)
		if err != nil {
			return err
		}
		if inserted != 1 {
			return ErrQuote
		}
		if err := record(ctx, q, id, pgtype.UUID{}, "payment_detected", "Simulated provider observation received"); err != nil {
			return err
		}
		if err := record(ctx, q, id, pgtype.UUID{}, "paid", "Simulated payment matched the quote"); err != nil {
			return err
		}
		thread, err := q.PurchaseConversation(ctx, db.PurchaseConversationParams{
			ListingID: order.ListingID, BuyerID: buyer, SellerID: order.SellerID,
		})
		if err != nil {
			return err
		}
		if err := q.EmitPurchaseEvent(ctx, db.EmitPurchaseEventParams{ConversationID: thread, OrderID: id, TitleSnapshot: order.Title}); err != nil {
			return err
		}
		if err := q.RecordLedgerSale(ctx, db.RecordLedgerSaleParams{Subject: id, Seller: order.SellerID, Code: order.Currency, Gross: order.PriceMinor, TestUnits: observation.Units}); err != nil {
			return err
		}
		return s.afterPayment(ctx, q, order)
	})
	if err == nil && expired {
		return ErrState
	}
	return err
}

func (s Service) Cancel(ctx context.Context, id, buyer pgtype.UUID) error {
	return s.transaction(ctx, func(q *db.Queries) error {
		row, err := locked(ctx, q, id, buyer)
		if err != nil {
			return err
		}
		if row.BuyerID != buyer {
			return pgx.ErrNoRows
		}
		if row.State == "cancelled" || row.State == "expired" {
			return nil
		}
		if row.State != "awaiting_payment" {
			return ErrState
		}
		state := "cancelled"
		if !time.Now().Before(row.CheckoutDeadline.Time) {
			state = "expired"
		}
		return release(ctx, q, row, buyer, state)
	})
}

func (s Service) SweepExpired(ctx context.Context) error {
	ids, err := db.New(s.Pool).ExpiredUnpaidOrders(ctx)
	if err != nil {
		return err
	}
	var failures error
	for _, id := range ids {
		err := s.transaction(ctx, func(q *db.Queries) error {
			row, err := q.LockOrder(ctx, id)
			if err != nil {
				return err
			}
			if row.State == "awaiting_payment" && !time.Now().Before(row.CheckoutDeadline.Time) {
				return release(ctx, q, row, pgtype.UUID{}, "expired")
			}
			return nil
		})
		failures = errors.Join(failures, err)
	}
	return failures
}

func release(ctx context.Context, q *db.Queries, order db.LockOrderRow, actor pgtype.UUID, state string) error {
	if order.StockReserved {
		if err := q.RestoreListingUnit(ctx, order.ListingID); err != nil {
			return err
		}
	}
	if err := q.SetOrderState(ctx, db.SetOrderStateParams{ID: order.ID, State: state}); err != nil {
		return err
	}
	return record(ctx, q, order.ID, actor, state, "Unpaid order closed and any reserved stock restored")
}

func (s Service) transaction(ctx context.Context, f func(*db.Queries) error) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error { return f(db.New(tx)) })
}

func locked(ctx context.Context, q *db.Queries, id, actor pgtype.UUID) (db.LockOrderRow, error) {
	row, err := q.LockOrder(ctx, id)
	if err == nil && actor != row.BuyerID && actor != row.SellerID {
		err = pgx.ErrNoRows
	}
	return row, err
}

func record(ctx context.Context, q *db.Queries, orderID, actor pgtype.UUID, state, reason string) error {
	return q.AddOrderStatusEvent(ctx, db.AddOrderStatusEventParams{OrderID: orderID, ActorID: actor, State: state, Reason: reason})
}

func transition(ctx context.Context, q *db.Queries, id, actor pgtype.UUID, state, reason string) error {
	if err := q.SetOrderState(ctx, db.SetOrderStateParams{ID: id, State: state}); err != nil {
		return err
	}
	return record(ctx, q, id, actor, state, reason)
}

func notify(ctx context.Context, q *db.Queries, id, recipient pgtype.UUID, kind string) error {
	return q.AddOrderNotification(ctx, db.AddOrderNotificationParams{OrderID: id, UserID: recipient, Kind: kind})
}

func timestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value, Valid: true}
}

func validText(value string, limit int) bool {
	return utf8.ValidString(value) && !strings.ContainsRune(value, 0) && utf8.RuneCountInString(value) <= limit && strings.TrimSpace(value) != ""
}
