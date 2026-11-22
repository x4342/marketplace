package finance

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
)

type PayoutProvider interface {
	QuoteUnits(int64) int64
	Observe(context.Context, pgtype.UUID, int64, int32, string) (PayoutObservation, error)
}

type PayoutObservation struct {
	Result    string
	Reference string
	Units     int64
}

type MockPayoutProvider struct{}

func (MockPayoutProvider) QuoteUnits(cents int64) int64 {
	return cents * 100
}

func (MockPayoutProvider) Observe(_ context.Context, id pgtype.UUID, units int64, attempt int32, scenario string) (PayoutObservation, error) {
	result := "confirmed"
	if attempt == 1 {
		switch scenario {
		case "fail_once":
			result = "failed"
		case "stall_once":
			result = "stalled"
		}
	}
	return PayoutObservation{Result: result, Reference: "sim-payout-" + id.String(), Units: units}, nil
}
