-- name: CheckoutListing :one
SELECT l.id, l.seller_id, l.title, l.kind, l.price_minor, l.currency, l.terms,
       l.delivery_mode, l.ship_regions, l.stock, l.can_backorder, l.digital_asset_id, l.status
FROM listings l WHERE l.id = $1 FOR UPDATE;

-- name: OrderByCheckoutKey :one
SELECT id, listing_id FROM orders WHERE buyer_id = $1 AND checkout_key = $2;

-- name: CreateUnpaidOrder :one
INSERT INTO orders (buyer_id,seller_id,listing_id,checkout_key,checkout_deadline)
VALUES ($1,$2,$3,$4,$5)
ON CONFLICT (buyer_id,checkout_key) DO NOTHING RETURNING id;

-- name: SnapshotOrderItem :exec
INSERT INTO order_items (order_id,title,kind,price_minor,currency,terms,delivery_mode,stock_reserved,asset_id)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9);

-- name: ReserveListingUnit :exec
UPDATE listings SET stock = stock - 1 WHERE id = $1;

-- name: RestoreListingUnit :exec
UPDATE listings SET stock = least(2147483647, stock::bigint + 1)::integer WHERE id = $1;

-- name: StoreShippingAddress :exec
INSERT INTO shipping_details (order_id,region,address) VALUES ($1,$2,$3);

-- name: OrderShipping :one
SELECT s.region,s.address FROM shipping_details s JOIN orders o ON o.id=s.order_id
WHERE o.id=sqlc.arg(order_id) AND sqlc.arg(actor)::uuid IN (o.buyer_id,o.seller_id)
AND (o.state NOT IN ('awaiting_payment','payment_detected','expired','cancelled') OR o.buyer_id=sqlc.arg(actor)::uuid);

-- name: UserOrders :many
SELECT o.id,o.state,i.title,o.created_at,o.buyer_id
FROM orders o JOIN order_items i ON i.order_id=o.id
WHERE sqlc.arg(actor)::uuid IN (o.buyer_id,o.seller_id)
ORDER BY o.created_at DESC,o.id LIMIT 31 OFFSET sqlc.arg(page_offset)::integer;

-- name: ParticipantOrder :one
SELECT o.id,o.buyer_id,o.seller_id,o.listing_id,o.state,o.checkout_deadline,o.created_at,
       i.title,i.kind,i.price_minor,i.currency,i.terms,i.delivery_mode,i.stock_reserved,i.asset_id
FROM orders o JOIN order_items i ON i.order_id=o.id
WHERE o.id=sqlc.arg(order_id) AND sqlc.arg(actor)::uuid IN (o.buyer_id,o.seller_id);

-- name: LockOrder :one
SELECT o.id,o.buyer_id,o.seller_id,o.listing_id,o.state,o.checkout_deadline,o.created_at,
       i.title,i.kind,i.price_minor,i.currency,i.terms,i.delivery_mode,i.stock_reserved,i.asset_id
FROM orders o JOIN order_items i ON i.order_id=o.id
WHERE o.id=$1 FOR UPDATE OF o;

-- name: SetOrderState :exec
UPDATE orders SET state=$2 WHERE id=$1;

-- name: AddOrderStatusEvent :exec
INSERT INTO order_status_events (order_id,actor_id,state,reason) VALUES ($1,$2,$3,$4);

-- name: OrderTimeline :many
SELECT e.state,e.reason,e.created_at,coalesce(u.username,'System')::text AS actor
FROM order_status_events e LEFT JOIN users u ON u.id=e.actor_id
JOIN orders o ON o.id=e.order_id
WHERE o.id=sqlc.arg(order_id) AND sqlc.arg(viewer)::uuid IN (o.buyer_id,o.seller_id)
ORDER BY e.id;

-- name: StoreQuote :exec
INSERT INTO quotes (order_id,provider_ref,units,asset,network,rate_numerator,fee_units,expires_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT (order_id) DO NOTHING;

-- name: OrderQuote :one
SELECT * FROM quotes WHERE order_id=$1;

-- name: StorePaymentObservation :execrows
INSERT INTO payment_observations (event_key,order_id,provider_ref,units,asset,network)
VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING;

-- name: PurchaseConversation :one
INSERT INTO conversations (listing_id,buyer_id,seller_id)
VALUES ($1,$2,$3)
ON CONFLICT (listing_id,buyer_id,seller_id) DO UPDATE SET updated_at=now()
RETURNING id;

-- name: EmitPurchaseEvent :exec
INSERT INTO conversation_system_events (conversation_id,order_id,kind,title_snapshot)
VALUES ($1,$2,'purchase',$3);

-- name: GrantDigitalEntitlement :exec
INSERT INTO digital_entitlements (order_id,buyer_id,asset_id) VALUES ($1,$2,$3);

-- name: BuyerDownload :one
SELECT a.payload FROM digital_entitlements e JOIN digital_assets a ON a.id=e.asset_id
JOIN orders o ON o.id=e.order_id
WHERE o.id=sqlc.arg(order_id) AND e.buyer_id=sqlc.arg(buyer)::uuid
AND o.state IN ('fulfilled','completed','disputed','under_review');

-- name: AddManualTask :exec
INSERT INTO manual_deliveries (order_id,deadline) VALUES ($1,$2);

-- name: ManualTask :one
SELECT d.deadline,coalesce(d.payload,'')::text AS payload
FROM manual_deliveries d JOIN orders o ON o.id=d.order_id
WHERE o.id=sqlc.arg(order_id) AND sqlc.arg(actor)::uuid IN (o.buyer_id,o.seller_id);

-- name: DeliverManualTask :exec
UPDATE manual_deliveries SET payload=$2 WHERE order_id=$1;

-- name: AddShipment :exec
INSERT INTO shipments (order_id,tracking_reference) VALUES ($1,$2);

-- name: OrderShipment :one
SELECT coalesce(s.tracking_reference,'')::text AS tracking FROM shipments s JOIN orders o ON o.id=s.order_id
WHERE o.id=sqlc.arg(order_id) AND sqlc.arg(actor)::uuid IN (o.buyer_id,o.seller_id);

-- name: AddOrderNotification :exec
INSERT INTO order_notifications (order_id,user_id,kind) VALUES ($1,$2,$3)
ON CONFLICT (order_id,user_id,kind) DO NOTHING;

-- name: UserOrderNotifications :many
SELECT n.order_id,n.kind,i.title,n.created_at FROM order_notifications n
JOIN orders o ON o.id=n.order_id JOIN order_items i ON i.order_id=o.id
WHERE n.user_id=sqlc.arg(actor)::uuid AND sqlc.arg(actor)::uuid IN (o.buyer_id,o.seller_id)
ORDER BY n.created_at DESC,n.id LIMIT 30 OFFSET sqlc.arg(page_offset)::integer;

-- name: ExpiredUnpaidOrders :many
SELECT id FROM orders WHERE state='awaiting_payment' AND checkout_deadline <= now()
ORDER BY checkout_deadline,id LIMIT 30;

-- name: OwnedAutomaticListing :one
SELECT title FROM listings
WHERE id=$1 AND seller_id=$2 AND kind='digital' AND delivery_mode='automatic' AND status IN ('published','paused')
FOR UPDATE;

-- name: StoreDigitalAsset :one
INSERT INTO digital_assets (listing_id,payload) VALUES ($1,$2) RETURNING id;

-- name: SetCurrentDigitalAsset :exec
UPDATE listings SET digital_asset_id=$2 WHERE id=$1;

-- name: OrderPurchaseConversation :one
SELECT e.conversation_id FROM conversation_system_events e JOIN orders o ON o.id=e.order_id
WHERE o.id=sqlc.arg(order_id) AND sqlc.arg(actor)::uuid IN (o.buyer_id,o.seller_id);
