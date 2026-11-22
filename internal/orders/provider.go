package orders

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/x4342/marketplace/internal/db"
)

// Provider is limited to the deterministic simulation in M5.
type Provider interface {
	Quote(context.Context, pgtype.UUID, int64, time.Time) (db.StoreQuoteParams, error)
	Observe(context.Context, db.Quote) (db.StorePaymentObservationParams, error)
}

type MockProvider struct{}

func (MockProvider) Quote(_ context.Context, id pgtype.UUID, cents int64, deadline time.Time) (db.StoreQuoteParams, error) {
	const unitsPerCent = 100
	return db.StoreQuoteParams{
		OrderID: id, ProviderRef: "test-" + id.String(), Units: cents * unitsPerCent,
		Asset: "TEST", Network: "simulation", RateNumerator: unitsPerCent, FeeUnits: 0,
		ExpiresAt: pgtype.Timestamptz{Time: deadline, Valid: true},
	}, nil
}

func (MockProvider) Observe(_ context.Context, quote db.Quote) (db.StorePaymentObservationParams, error) {
	return db.StorePaymentObservationParams{
		EventKey: "confirmed:" + quote.ProviderRef, OrderID: quote.OrderID, ProviderRef: quote.ProviderRef,
		Units: quote.Units, Asset: quote.Asset, Network: quote.Network,
	}, nil
}
