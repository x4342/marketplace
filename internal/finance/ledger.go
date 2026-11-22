package finance

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/x4342/marketplace/internal/db"
)

var (
	ErrFunds   = errors.New("insufficient funds")
	ErrState   = errors.New("finance action unavailable")
	ErrInvalid = errors.New("invalid finance input")
	ErrRole    = errors.New("finance role required")
)

// Commission applies the proposed merchandise-subtotal base and half-up rounding in simulation.
func Commission(gross int64) int64 {
	return (gross*5 + 50) / 100
}

func MoveSale(ctx context.Context, q *db.Queries, order db.LockOrderRow, destination string) error {
	net := order.PriceMinor - Commission(order.PriceMinor)
	return post(ctx, q, destinationKey(destination), order.ID, order.SellerID, order.Currency,
		[]string{"pending", destination}, []int64{-net, net})
}

func destinationKey(destination string) string {
	if destination == "held" {
		return "hold"
	}
	return "release"
}

func RefundSale(ctx context.Context, q *db.Queries, order db.LockOrderRow, kind string) error {
	source := "pending"
	switch order.State {
	case "completed":
		source = "available"
	case "fulfillment_failed", "disputed", "under_review":
		source = "held"
	case "fulfillment_pending", "fulfilled", "paid":
	default:
		return ErrState
	}
	units, err := q.FirstOrderObservation(ctx, order.ID)
	if err != nil {
		return err
	}
	fee := Commission(order.PriceMinor)
	net := order.PriceMinor - fee
	if err := post(ctx, q, kind, order.ID, order.SellerID, order.Currency,
		[]string{"sale_clearing", source, "commission"}, []int64{order.PriceMinor, -net, -fee}); err != nil {
		return err
	}
	return post(ctx, q, kind+"-test", order.ID, pgtype.UUID{}, "TEST",
		[]string{"test_treasury", "test_provider"}, []int64{-units, units})
}

func post(ctx context.Context, q *db.Queries, key string, id, owner pgtype.UUID, code string, buckets []string, amounts []int64) error {
	_, err := q.PostJournal(ctx, db.PostJournalParams{
		EventKey: key + ":" + id.String(), Subject: id, Owner: owner, Code: code, Buckets: buckets, Amounts: amounts,
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "P7001" {
		return ErrFunds
	}
	return err
}

// ReleaseDisputedSale moves the sandbox net proceeds out of the held bucket.
func ReleaseDisputedSale(ctx context.Context, q *db.Queries, order db.LockOrderRow) error {
	net := order.PriceMinor - Commission(order.PriceMinor)
	return post(ctx, q, "dispute-release", order.ID, order.SellerID, order.Currency,
		[]string{"held", "available"}, []int64{-net, net})
}
