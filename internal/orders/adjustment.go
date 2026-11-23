package orders

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/x4342/marketplace/internal/authz"
	"github.com/x4342/marketplace/internal/db"
	"github.com/x4342/marketplace/internal/finance"
)

// Adjust applies a full sandbox refund or simulated chain reversal with compensating journals.
func (s Service) Adjust(ctx context.Context, admin, id pgtype.UUID, kind, reason string) error {
	if (kind != "refund" && kind != "reversal") || !finance.ValidReason(reason) {
		return finance.ErrInvalid
	}
	return s.transaction(ctx, func(q *db.Queries) error {
		role, err := q.EffectiveRole(ctx, admin)
		if err != nil {
			return err
		}
		if !authz.Allows(role, authz.Staff) {
			return finance.ErrRole
		}
		order, err := q.LockOrder(ctx, id)
		if err != nil {
			return err
		}
		if admin == order.SellerID || admin == order.BuyerID {
			return finance.ErrRole
		}
		if order.State == "refunded" {
			return nil
		}
		if _, err := q.OrderDispute(ctx, id); err == nil {
			return finance.ErrState
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err := finance.RefundSale(ctx, q, order, kind); err != nil {
			return err
		}
		label := "Simulated full refund"
		if kind == "reversal" {
			label = "Simulated chain reversal"
		}
		if err := transition(ctx, q, id, admin, "refunded", label); err != nil {
			return err
		}
		return q.PostAdminAudit(ctx, db.PostAdminAuditParams{ActorID: admin, Area: "payment", SubjectID: id, Action: kind, Reason: strings.TrimSpace(reason)})
	})
}
