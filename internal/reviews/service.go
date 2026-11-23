package reviews

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/x4342/marketplace/internal/authz"
	"github.com/x4342/marketplace/internal/db"
)

const MinimumRatings = 3

var ErrConflict = errors.New("review action unavailable")
var ErrInvalid = errors.New("invalid review fields")

type Service struct {
	Pool *pgxpool.Pool
}

func (s Service) Create(ctx context.Context, actor, order pgtype.UUID, stars int16, body string) error {
	if stars < 1 || stars > 5 || !valid(body, 2000) {
		return ErrInvalid
	}
	return s.tx(ctx, func(q *db.Queries) error {
		row, err := q.LockOrder(ctx, order)
		if err != nil {
			return err
		}
		if row.BuyerID != actor {
			return pgx.ErrNoRows
		}
		review, err := q.CreateVerifiedReview(ctx, db.CreateVerifiedReviewParams{Actor: actor, OrderID: order, Stars: stars, Body: strings.TrimSpace(body)})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrConflict
		}
		if err != nil {
			return err
		}
		return notify(ctx, q, review.ID, review.SellerID, "received")
	})
}

func (s Service) Report(ctx context.Context, actor, id pgtype.UUID, reason string) (pgtype.UUID, error) {
	if !valid(reason, 1000) {
		return pgtype.UUID{}, ErrInvalid
	}
	var listing pgtype.UUID
	err := s.tx(ctx, func(q *db.Queries) error {
		row, err := q.LockReview(ctx, id)
		if err != nil {
			return err
		}
		if row.Hidden {
			return pgx.ErrNoRows
		}
		if row.AuthorID == actor {
			return ErrConflict
		}
		listing = row.ListingID
		rows, err := q.AddReviewReport(ctx, db.AddReviewReportParams{ReviewID: id, ReporterID: actor, Reason: strings.TrimSpace(reason)})
		return changed(rows, err)
	})
	return listing, err
}

func (s Service) Appeal(ctx context.Context, actor, id pgtype.UUID, reason string) error {
	if !valid(reason, 1000) {
		return ErrInvalid
	}
	return s.tx(ctx, func(q *db.Queries) error {
		row, err := q.LockReview(ctx, id)
		if err != nil {
			return err
		}
		if row.AuthorID != actor {
			return pgx.ErrNoRows
		}
		if !row.Hidden {
			return ErrConflict
		}
		count, err := q.AddReviewAppeal(ctx, db.AddReviewAppealParams{ReviewID: id, AppellantID: actor, Reason: strings.TrimSpace(reason)})
		return changed(count, err)
	})
}

func (s Service) Decide(ctx context.Context, moderator, id pgtype.UUID, action, reason string) error {
	if !valid(reason, 1000) {
		return ErrInvalid
	}
	return s.tx(ctx, func(q *db.Queries) error {
		role, err := q.EffectiveRole(ctx, moderator)
		if err != nil {
			return err
		}
		if !authz.Allows(role, authz.Staff) {
			return ErrConflict
		}
		row, err := q.LockReview(ctx, id)
		if err != nil {
			return err
		}
		if moderator == row.AuthorID || moderator == row.SellerID {
			return ErrConflict
		}
		var count int64
		switch action {
		case "dismiss", "hide":
			if row.Hidden {
				return ErrConflict
			}
			count, err = q.ResolveReviewReports(ctx, id)
		case "uphold", "restore":
			hider, err := q.LatestReviewHider(ctx, id)
			if err != nil {
				return err
			}
			if hider == moderator {
				return ErrConflict
			}
			if !row.Hidden {
				return ErrConflict
			}
			count, err = q.ResolveReviewAppeal(ctx, id)
		default:
			return ErrInvalid
		}
		if err := changed(count, err); err != nil {
			return err
		}
		if action == "hide" || action == "restore" {
			if err := q.SetReviewVisibility(ctx, db.SetReviewVisibilityParams{ID: id, Hidden: action == "hide"}); err != nil {
				return err
			}
		}
		if err := q.AuditReviewAction(ctx, db.AuditReviewActionParams{ReviewID: id, ModeratorID: moderator, Action: action, Reason: strings.TrimSpace(reason)}); err != nil {
			return err
		}
		switch action {
		case "hide":
			return notify(ctx, q, id, row.AuthorID, "hidden")
		case "uphold":
			return notify(ctx, q, id, row.AuthorID, "upheld")
		case "restore":
			return notify(ctx, q, id, row.AuthorID, "restored")
		}
		return nil
	})
}

func notify(ctx context.Context, q *db.Queries, id, actor pgtype.UUID, kind string) error {
	return q.NotifyReview(ctx, db.NotifyReviewParams{ReviewID: id, UserID: actor, Kind: kind})
}

func changed(count int64, err error) error {
	if err == nil && count == 0 {
		return ErrConflict
	}
	return err
}

func valid(value string, limit int) bool {
	return strings.TrimSpace(value) != "" && utf8.ValidString(value) && !strings.ContainsRune(value, 0) && utf8.RuneCountInString(value) <= limit
}

func (s Service) tx(ctx context.Context, f func(*db.Queries) error) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error { return f(db.New(tx)) })
}
