package moderation

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

var ErrRole = errors.New("moderator role required")
var ErrState = errors.New("case action unavailable")
var ErrInvalid = errors.New("case input invalid")

type Service struct{ Pool *pgxpool.Pool }

func (s Service) Report(ctx context.Context, actor, listing pgtype.UUID, kind, reason string) error {
	if !valid(reason, 1000) || (kind != "listing" && kind != "seller") {
		return ErrInvalid
	}
	return s.transact(ctx, func(q *db.Queries) error {
		target, err := q.PublicContentTarget(ctx, listing)
		if err != nil {
			return err
		}
		if target.SellerID == actor {
			return ErrState
		}
		params := db.ReportListingOrSellerParams{ReporterID: actor, Reason: strings.TrimSpace(reason)}
		if kind == "listing" {
			params.ListingID = listing
		} else {
			params.SubjectUserID = target.SellerID
		}
		count, err := q.ReportListingOrSeller(ctx, params)
		return applied(count, err)
	})
}

func (s Service) ContentAppeal(ctx context.Context, actor, id pgtype.UUID, reason string) error {
	if !valid(reason, 1000) {
		return ErrInvalid
	}
	return s.transact(ctx, func(q *db.Queries) error {
		row, err := q.ContentReportRecord(ctx, id)
		if err != nil {
			return err
		}
		if !affected(row, actor) {
			return pgx.ErrNoRows
		}
		if row.State != "restricted" {
			return ErrState
		}
		count, err := q.AddContentAppeal(ctx, db.AddContentAppealParams{ReportID: id, ActorID: actor, Reason: strings.TrimSpace(reason)})
		return applied(count, err)
	})
}

func (s Service) ContentDecide(ctx context.Context, actor, id pgtype.UUID, action, reason string) error {
	if !valid(reason, 1000) {
		return ErrInvalid
	}
	return s.transact(ctx, func(q *db.Queries) error {
		if err := authorize(ctx, q, actor); err != nil {
			return err
		}
		row, err := q.ContentReportRecord(ctx, id)
		if err != nil {
			return err
		}
		if row.ReporterID == actor || affected(row, actor) {
			return ErrRole
		}
		target := row.ListingID
		if !target.Valid {
			target = row.SubjectUserID
		}
		if !target.Valid {
			target = row.ConversationID
		}
		if err := q.LockContentTarget(ctx, target); err != nil {
			return err
		}
		var state string
		switch action {
		case "dismiss", "restrict":
			if row.State != "open" {
				return ErrState
			}
			state = "dismissed"
			if action == "restrict" {
				state = "restricted"
				if err := restriction(ctx, q, row, actor, true, reason); err != nil {
					return err
				}
			}
		case "restore", "uphold":
			if row.State != "restricted" || row.DecidedBy == actor {
				return ErrState
			}
			count, err := q.ResolveContentAppeals(ctx, id)
			if err := applied(count, err); err != nil {
				return err
			}
			state = "upheld"
			if action == "restore" {
				state = "restored"
				other, err := q.OtherContentRestriction(ctx, id)
				if err != nil {
					return err
				}
				if !other {
					if err := restriction(ctx, q, row, actor, false, reason); err != nil {
						return err
					}
				}
			}
		default:
			return ErrInvalid
		}
		if err := q.SetContentOutcome(ctx, db.SetContentOutcomeParams{ID: id, State: state, DecidedBy: actor}); err != nil {
			return err
		}
		return audit(ctx, q, actor, id, "content", action, reason)
	})
}

func restriction(ctx context.Context, q *db.Queries, row db.ContentReportRecordRow, actor pgtype.UUID, block bool, reason string) error {
	switch {
	case row.ListingID.Valid:
		return q.SetListingModeration(ctx, db.SetListingModerationParams{ID: row.ListingID, ModerationLocked: block})
	case row.ConversationID.Valid:
		return q.SetConversationModeration(ctx, db.SetConversationModerationParams{ID: row.ConversationID, ModerationBlocked: block})
	case block:
		return q.RestrictUser(ctx, db.RestrictUserParams{UserID: row.SubjectUserID, ModeratorID: actor, Reason: strings.TrimSpace(reason)})
	default:
		return q.UnrestrictUser(ctx, row.SubjectUserID)
	}
}

func affected(row db.ContentReportRecordRow, actor pgtype.UUID) bool {
	return row.Affected == actor || row.ConversationBuyer == actor
}

func (s Service) Submit(ctx context.Context, actor, id pgtype.UUID, kind, body string) error {
	if !valid(body, 2000) || (kind != "response" && kind != "evidence") {
		return ErrInvalid
	}
	return s.transact(ctx, func(q *db.Queries) error {
		row, err := participant(ctx, q, actor, id)
		if err != nil {
			return err
		}
		if row.Status != "open" && row.Status != "hold" {
			return ErrState
		}
		if err := q.AddCaseEvidence(ctx, db.AddCaseEvidenceParams{DisputeID: id, ActorID: actor, Kind: kind, Body: strings.TrimSpace(body)}); err != nil {
			return err
		}
		return q.NotifyDispute(ctx, db.NotifyDisputeParams{DisputeID: id, Label: "Dispute evidence or response added"})
	})
}

func (s Service) DisputeAppeal(ctx context.Context, actor, id pgtype.UUID, reason string) error {
	if !valid(reason, 1000) {
		return ErrInvalid
	}
	return s.transact(ctx, func(q *db.Queries) error {
		row, err := participant(ctx, q, actor, id)
		if err != nil {
			return err
		}
		if row.Status != "released" && row.Status != "refunded" {
			return ErrState
		}
		count, err := q.AddDisputeAppeal(ctx, db.AddDisputeAppealParams{DisputeID: id, ActorID: actor, Reason: strings.TrimSpace(reason)})
		if err := applied(count, err); err != nil {
			return err
		}
		return disputeEvent(ctx, q, actor, id, "appealed", "A participant submitted an appeal")
	})
}

func (s Service) DisputeAppealDecision(ctx context.Context, actor, id, appealID pgtype.UUID, action, reason string) error {
	if !valid(reason, 1000) || (action != "upheld" && action != "referred") {
		return ErrInvalid
	}
	return s.transact(ctx, func(q *db.Queries) error {
		if err := authorize(ctx, q, actor); err != nil {
			return err
		}
		d, err := q.LockDispute(ctx, id)
		if err != nil {
			return err
		}
		row, err := q.DisputeRecord(ctx, id)
		if err != nil {
			return err
		}
		if d.DecidedBy == actor || actor == row.BuyerID || actor == row.SellerID {
			return ErrRole
		}
		count, err := q.ResolveDisputeAppeal(ctx, db.ResolveDisputeAppealParams{ID: appealID, DisputeID: id, Outcome: action})
		if err := applied(count, err); err != nil {
			return err
		}
		if err := audit(ctx, q, actor, id, "dispute", "appeal_"+action, reason); err != nil {
			return err
		}
		return disputeEvent(ctx, q, actor, id, "appeal_"+action, reason)
	})
}

func participant(ctx context.Context, q *db.Queries, actor, id pgtype.UUID) (db.DisputeRecordRow, error) {
	if _, err := q.LockDispute(ctx, id); err != nil {
		return db.DisputeRecordRow{}, err
	}
	row, err := q.DisputeRecord(ctx, id)
	if err == nil && actor != row.BuyerID && actor != row.SellerID {
		err = pgx.ErrNoRows
	}
	return row, err
}

func disputeEvent(ctx context.Context, q *db.Queries, actor, id pgtype.UUID, action, message string) error {
	if err := q.PostDisputeEvent(ctx, db.PostDisputeEventParams{DisputeID: id, ActorID: actor, Action: action, Detail: strings.TrimSpace(message)}); err != nil {
		return err
	}
	return q.NotifyDispute(ctx, db.NotifyDisputeParams{DisputeID: id, Label: "Dispute appeal updated"})
}

func audit(ctx context.Context, q *db.Queries, actor, id pgtype.UUID, area, action, reason string) error {
	return q.PostAdminAudit(ctx, db.PostAdminAuditParams{ActorID: actor, SubjectID: id, Area: area, Action: action, Reason: strings.TrimSpace(reason)})
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

func applied(count int64, err error) error {
	if err == nil && count == 0 {
		return ErrState
	}
	return err
}

func valid(value string, limit int) bool {
	return strings.TrimSpace(value) != "" && utf8.ValidString(value) && !strings.ContainsRune(value, 0) && utf8.RuneCountInString(value) <= limit
}

func (s Service) transact(ctx context.Context, f func(*db.Queries) error) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error { return f(db.New(tx)) })
}
