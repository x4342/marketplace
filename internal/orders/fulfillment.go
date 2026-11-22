package orders

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/x4342/marketplace/internal/db"
	"github.com/x4342/marketplace/internal/finance"
)

func (s Service) afterPayment(ctx context.Context, q *db.Queries, order db.LockOrderRow) error {
	id := order.ID
	system := pgtype.UUID{}
	if err := transition(ctx, q, id, system, "fulfillment_pending", "Fulfillment started"); err != nil {
		return err
	}
	if err := notify(ctx, q, id, order.BuyerID, "paid"); err != nil {
		return err
	}
	switch order.DeliveryMode {
	case "automatic":
		if err := q.GrantDigitalEntitlement(ctx, db.GrantDigitalEntitlementParams{OrderID: id, BuyerID: order.BuyerID, AssetID: order.AssetID}); err != nil {
			return err
		}
		if err := transition(ctx, q, id, system, "fulfilled", "Private download made available"); err != nil {
			return err
		}
		return notify(ctx, q, id, order.BuyerID, "download")
	case "manual":
		if err := q.AddManualTask(ctx, db.AddManualTaskParams{OrderID: id, Deadline: timestamp(time.Now().Add(s.ManualWindow))}); err != nil {
			return err
		}
		return notify(ctx, q, id, order.SellerID, "manual_task")
	default:
		return notify(ctx, q, id, order.SellerID, "paid")
	}
}

func (s Service) Act(ctx context.Context, id, actor pgtype.UUID, action, input string) error {
	return s.transaction(ctx, func(q *db.Queries) error {
		order, err := locked(ctx, q, id, actor)
		if err != nil {
			return err
		}
		switch action {
		case "dispatch":
			if actor != order.SellerID || order.Kind != "physical" || order.State != "fulfillment_pending" ||
				(input != "" && !validText(input, 100)) {
				return ErrState
			}
			if err := q.AddShipment(ctx, db.AddShipmentParams{OrderID: id, TrackingReference: pgtype.Text{String: input, Valid: input != ""}}); err != nil {
				return err
			}
			if err := transition(ctx, q, id, actor, "fulfilled", "Seller marked the shipment dispatched"); err != nil {
				return err
			}
			return notify(ctx, q, id, order.BuyerID, "shipment")
		case "deliver":
			if actor != order.SellerID || order.DeliveryMode != "manual" || order.State != "fulfillment_pending" || !validText(input, 2000) {
				return ErrState
			}
			if err := q.DeliverManualTask(ctx, db.DeliverManualTaskParams{OrderID: id, Payload: pgtype.Text{String: input, Valid: true}}); err != nil {
				return err
			}
			if err := transition(ctx, q, id, actor, "fulfilled", "Seller submitted manual delivery"); err != nil {
				return err
			}
			return notify(ctx, q, id, order.BuyerID, "manual_ready")
		case "complete":
			if actor != order.BuyerID || order.State != "fulfilled" {
				return ErrState
			}
			if err := finance.MoveSale(ctx, q, order, "available"); err != nil {
				return err
			}
			return transition(ctx, q, id, actor, "completed", "Buyer confirmed receipt")
		case "problem":
			if actor != order.BuyerID || (order.State != "fulfillment_pending" && order.State != "fulfilled" && order.State != "fulfillment_failed") {
				return ErrState
			}
			if order.State != "fulfillment_failed" {
				if err := finance.MoveSale(ctx, q, order, "held"); err != nil {
					return err
				}
			}
			if err := transition(ctx, q, id, actor, "disputed", "Buyer flagged a fulfillment problem"); err != nil {
				return err
			}
			return notify(ctx, q, id, order.SellerID, "problem")
		case "failed":
			if actor != order.SellerID || order.State != "fulfillment_pending" {
				return ErrState
			}
			if err := finance.MoveSale(ctx, q, order, "held"); err != nil {
				return err
			}
			if err := transition(ctx, q, id, actor, "fulfillment_failed", "Seller reported a fulfillment failure"); err != nil {
				return err
			}
			return notify(ctx, q, id, order.BuyerID, "problem")
		default:
			return ErrState
		}
	})
}

func (s Service) SetAsset(ctx context.Context, listingID, sellerID pgtype.UUID, payload string) error {
	if len(payload) > 65536 || !validText(payload, 65536) {
		return ErrState
	}
	return s.transaction(ctx, func(q *db.Queries) error {
		if _, err := q.OwnedAutomaticListing(ctx, db.OwnedAutomaticListingParams{ID: listingID, SellerID: sellerID}); err != nil {
			return err
		}
		id, err := q.StoreDigitalAsset(ctx, db.StoreDigitalAssetParams{ListingID: listingID, Payload: payload})
		if err != nil {
			return err
		}
		return q.SetCurrentDigitalAsset(ctx, db.SetCurrentDigitalAssetParams{ID: listingID, DigitalAssetID: id})
	})
}
