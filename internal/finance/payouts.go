package finance

import (
	"context"
	"crypto/sha256"
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/x4342/marketplace/internal/authz"
	"github.com/x4342/marketplace/internal/db"
)

type Service struct {
	Pool     *pgxpool.Pool
	Provider PayoutProvider
}

func Simulation(pool *pgxpool.Pool) Service {
	return Service{Pool: pool, Provider: MockPayoutProvider{}}
}

var decimal = regexp.MustCompile(`^[0-9]{1,9}(\.[0-9]{1,2})?$`)

func ParseAmount(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if !decimal.MatchString(raw) {
		return 0, ErrInvalid
	}
	whole, part, _ := strings.Cut(raw, ".")
	major, _ := strconv.ParseInt(whole, 10, 64)
	minor, _ := strconv.ParseInt((part + "00")[:2], 10, 64)
	cents := 100*major + minor
	if cents <= 0 {
		return 0, ErrInvalid
	}
	return cents, nil
}

func (s Service) Request(ctx context.Context, seller pgtype.UUID, code string, cents int64, key, scenario string) (pgtype.UUID, error) {
	if (code != "USD" && code != "EUR") || cents < 1 || cents > 100000000000 || key == "" ||
		!slices.Contains([]string{"success", "fail_once", "stall_once"}, scenario) {
		return pgtype.UUID{}, ErrInvalid
	}
	digest := sha256.Sum256([]byte(key))
	var id pgtype.UUID
	err := s.tx(ctx, func(q *db.Queries) error {
		if err := q.LockSellerFunds(ctx, seller); err != nil {
			return err
		}
		existing, err := q.PayoutByKey(ctx, db.PayoutByKeyParams{SellerID: seller, RequestKey: digest[:]})
		if err == nil {
			if existing.Currency != code || existing.AmountMinor != cents || existing.Scenario != scenario {
				return ErrState
			}
			id = existing.ID
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		id, err = q.InsertPayout(ctx, db.InsertPayoutParams{SellerID: seller, RequestKey: digest[:],
			Currency: code, AmountMinor: cents, TestUnits: s.Provider.QuoteUnits(cents), Scenario: scenario})
		if err != nil {
			return err
		}
		if err := post(ctx, q, "reserve", id, seller, code, []string{"available", "reserved"}, []int64{-cents, cents}); err != nil {
			return err
		}
		return audit(ctx, q, id, seller, "reserved", "Simulated payout requested and reserved")
	})
	return id, err
}

func (s Service) Act(ctx context.Context, admin, id pgtype.UUID, action, reason string) error {
	reason = strings.TrimSpace(reason)
	if !ValidReason(reason) {
		return ErrInvalid
	}
	return s.tx(ctx, func(q *db.Queries) error {
		if err := authorize(ctx, q, admin); err != nil {
			return err
		}
		p, err := q.LockPayout(ctx, id)
		if err != nil {
			return err
		}
		if p.SellerID == admin {
			return ErrRole
		}

		if action == "hold" || action == "lift_hold" {
			return payoutHold(ctx, q, p, admin, action, reason)
		}
		next, attempt := p.State, p.Attempts
		switch action {
		case "review":
			if p.State != "reserved" {
				return ErrState
			}
			next = "under_review"
		case "approve":
			if p.State != "under_review" || p.HoldActive {
				return ErrState
			}
			next = "approved"
		case "deny":
			if p.State != "under_review" && p.State != "failed" && p.State != "stalled" {
				return ErrState
			}
			destination := "available"
			if p.HoldActive {
				destination = "held"
			}
			if err := post(ctx, q, "denial", id, p.SellerID, p.Currency, []string{"reserved", destination}, []int64{-p.AmountMinor, p.AmountMinor}); err != nil {
				return err
			}
			next = "denied"
		case "process":
			if p.HoldActive || (p.State != "approved" && p.State != "failed" && p.State != "stalled") {
				return ErrState
			}
			next, attempt = "processing", attempt+1
		case "observe":
			if p.State == "confirmed" || p.State == "reconciled" {
				return nil
			}
			if p.State != "processing" {
				return ErrState
			}
			observation, err := s.Provider.Observe(ctx, id, p.TestUnits, attempt, p.Scenario)
			if err != nil {
				return err
			}
			if observation.Units != p.TestUnits || observation.Reference == "" {
				return ErrState
			}
			next = observation.Result
			switch next {
			case "confirmed":
				if err := settle(ctx, q, p, observation); err != nil {
					return err
				}
			case "failed", "stalled":
			default:
				return ErrState
			}
		case "reconcile":
			if p.State == "reconciled" {
				return nil
			}
			if p.State != "confirmed" {
				return ErrState
			}
			item, err := q.PayoutReconciliation(ctx, id)
			if err != nil {
				return err
			}
			if item.SettledUnits != p.TestUnits {
				return ErrState
			}
			next = "reconciled"
		default:
			return ErrInvalid
		}
		if err := q.SetPayoutState(ctx, db.SetPayoutStateParams{ID: id, State: next, Attempts: attempt}); err != nil {
			return err
		}
		return audit(ctx, q, id, admin, next, reason)
	})
}

func settle(ctx context.Context, q *db.Queries, p db.SellerPayout, o PayoutObservation) error {
	if err := post(ctx, q, "payout-fiat", p.ID, p.SellerID, p.Currency, []string{"reserved", "withdrawn"}, []int64{-p.AmountMinor, p.AmountMinor}); err != nil {
		return err
	}
	if err := post(ctx, q, "payout-conversion-test", p.ID, pgtype.UUID{}, "TEST",
		[]string{"test_conversion", "test_treasury"}, []int64{-o.Units, o.Units}); err != nil {
		return err
	}
	if err := post(ctx, q, "payout-settlement-test", p.ID, pgtype.UUID{}, "TEST",
		[]string{"test_treasury", "test_external"}, []int64{-o.Units, o.Units}); err != nil {
		return err
	}
	return q.RecordReconciliation(ctx, db.RecordReconciliationParams{PayoutID: p.ID, ProviderRef: o.Reference, SettledUnits: o.Units})
}

func audit(ctx context.Context, q *db.Queries, payout, actor pgtype.UUID, action, reason string) error {
	return q.AuditPayout(ctx, db.AuditPayoutParams{PayoutID: payout, ActorID: actor, Action: action, Reason: reason})
}

func authorize(ctx context.Context, q *db.Queries, actor pgtype.UUID) error {
	role, err := q.EffectiveRole(ctx, actor)
	if err != nil {
		return err
	}
	if !authz.Allows(role, authz.Staff) {
		return ErrRole
	}
	return nil
}

func ValidReason(reason string) bool {
	return strings.TrimSpace(reason) != "" && len(reason) <= 4000 && utf8.ValidString(reason) &&
		utf8.RuneCountInString(reason) <= 1000 && !strings.ContainsRune(reason, 0)
}

func (s Service) tx(ctx context.Context, f func(*db.Queries) error) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error { return f(db.New(tx)) })
}

func payoutHold(ctx context.Context, q *db.Queries, p db.SellerPayout, admin pgtype.UUID, action, reason string) error {
	active := action == "hold"
	if p.HoldActive == active || p.State == "processing" || p.State == "confirmed" || p.State == "reconciled" || (p.State == "denied" && active) {
		return ErrState
	}
	if p.State == "denied" {
		if err := post(ctx, q, "lift-denial-hold", p.ID, p.SellerID, p.Currency, []string{"held", "available"}, []int64{-p.AmountMinor, p.AmountMinor}); err != nil {
			return err
		}
	}
	if err := q.SetPayoutHold(ctx, db.SetPayoutHoldParams{ID: p.ID, HoldActive: active}); err != nil {
		return err
	}
	label := "hold_lifted"
	if active {
		label = "hold_set"
	}
	return audit(ctx, q, p.ID, admin, label, reason)
}
