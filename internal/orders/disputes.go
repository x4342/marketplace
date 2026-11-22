package orders

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/x4342/marketplace/internal/db"
	"github.com/x4342/marketplace/internal/finance"
)

func Complaint(input string) (string, string, bool) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "other", "Buyer flagged a fulfillment problem", true
	}
	category, reason, ok := strings.Cut(input, "\n")
	if !ok || !validText(reason, 2000) {
		return "", "", false
	}
	switch category {
	case "not_received", "not_as_described", "delivery_failed", "other":
		return category, strings.TrimSpace(reason), true
	default:
		return "", "", false
	}
}

func openDispute(ctx context.Context, q *db.Queries, orderID, buyer pgtype.UUID, category, reason string) error {
	id, err := q.OpenOrderDispute(ctx, db.OpenOrderDisputeParams{OrderID: orderID, Category: category})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrState
	}
	if err != nil {
		return err
	}
	if err := q.PostDisputeEvent(ctx, db.PostDisputeEventParams{DisputeID: id, ActorID: buyer, Action: "opened", Detail: reason}); err != nil {
		return err
	}
	return q.NotifyDispute(ctx, db.NotifyDisputeParams{DisputeID: id, Label: "Dispute opened"})
}

func (s Service) ResolveDispute(ctx context.Context, actor, caseID pgtype.UUID, action, reason string) error {
	if !finance.ValidReason(reason) {
		return finance.ErrInvalid
	}
	return s.transaction(ctx, func(q *db.Queries) error {
		ok, err := q.ContentRole(ctx, actor)
		if err != nil {
			return err
		}
		if !ok {
			return finance.ErrRole
		}
		dispute, err := q.LockDispute(ctx, caseID)
		if err != nil {
			return err
		}
		order, err := q.LockOrder(ctx, dispute.OrderID)
		if err != nil {
			return err
		}
		if actor == order.BuyerID || actor == order.SellerID {
			return finance.ErrRole
		}
		if (dispute.Status != "open" && dispute.Status != "hold") || (order.State != "disputed" && order.State != "under_review") {
			return ErrState
		}
		outcome, orderState, label := "", "", ""
		switch action {
		case "hold":
			if dispute.Status == "hold" {
				return ErrState
			}
			outcome, orderState, label = "hold", "under_review", "Dispute funds remain held"
		case "release":
			if err := finance.ReleaseDisputedSale(ctx, q, order); err != nil {
				return err
			}
			outcome, orderState, label = "released", "completed", "Dispute resolved with a sandbox release"
		case "refund":
			if err := finance.RefundSale(ctx, q, order, "dispute-refund"); err != nil {
				return err
			}
			outcome, orderState, label = "refunded", "refunded", "Dispute resolved with a sandbox refund"
		default:
			return finance.ErrInvalid
		}
		if err := q.SetDisputeOutcome(ctx, db.SetDisputeOutcomeParams{ID: caseID, Status: outcome, DecidedBy: actor}); err != nil {
			return err
		}
		if err := transition(ctx, q, order.ID, actor, orderState, label); err != nil {
			return err
		}
		if err := q.PostDisputeEvent(ctx, db.PostDisputeEventParams{DisputeID: caseID, ActorID: actor, Action: action, Detail: strings.TrimSpace(reason)}); err != nil {
			return err
		}
		if err := q.PostAdminAudit(ctx, db.PostAdminAuditParams{ActorID: actor, Area: "dispute", SubjectID: caseID, Action: action, Reason: strings.TrimSpace(reason)}); err != nil {
			return err
		}
		return q.NotifyDispute(ctx, db.NotifyDisputeParams{DisputeID: caseID, Label: label})
	})
}
