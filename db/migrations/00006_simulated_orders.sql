-- +goose Up
CREATE TABLE digital_assets (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    listing_id uuid NOT NULL REFERENCES listings(id),
    payload text NOT NULL CHECK (octet_length(payload) BETWEEN 1 AND 65536),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (listing_id, id)
);
ALTER TABLE listings ADD COLUMN digital_asset_id uuid;
ALTER TABLE listings ADD CONSTRAINT listing_digital_asset_fk
    FOREIGN KEY (id, digital_asset_id) REFERENCES digital_assets(listing_id, id);

CREATE TABLE orders (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    buyer_id uuid NOT NULL REFERENCES users(id),
    seller_id uuid NOT NULL REFERENCES users(id),
    listing_id uuid NOT NULL REFERENCES listings(id),
    checkout_key bytea NOT NULL CHECK (octet_length(checkout_key) = 32),
    checkout_deadline timestamptz NOT NULL,
    state text NOT NULL DEFAULT 'awaiting_payment' CHECK (state IN (
        'awaiting_payment','payment_detected','paid','fulfillment_pending','fulfilled',
        'completed','expired','cancelled','fulfillment_failed','disputed','under_review','refunded'
    )),
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (buyer_id <> seller_id),
    UNIQUE(buyer_id, checkout_key)
);
CREATE INDEX orders_buyer_idx ON orders (buyer_id, created_at DESC, id);
CREATE INDEX orders_seller_idx ON orders (seller_id, created_at DESC, id);
CREATE INDEX orders_expiry_idx ON orders (checkout_deadline, id) WHERE state = 'awaiting_payment';

CREATE TABLE order_items (
    order_id uuid PRIMARY KEY REFERENCES orders(id),
    title text NOT NULL,
    kind text NOT NULL CHECK (kind IN ('physical','digital')),
    price_minor bigint NOT NULL CHECK (price_minor > 0),
    currency text NOT NULL CHECK (currency IN ('USD','EUR')),
    terms text NOT NULL,
    delivery_mode text NOT NULL,
    stock_reserved boolean NOT NULL,
    asset_id uuid REFERENCES digital_assets(id),
    CHECK ((kind = 'physical' AND delivery_mode = '' AND asset_id IS NULL)
        OR (kind = 'digital' AND ((delivery_mode = 'manual' AND asset_id IS NULL)
            OR (delivery_mode = 'automatic' AND asset_id IS NOT NULL))))
);

CREATE TABLE shipping_details (
    order_id uuid PRIMARY KEY REFERENCES orders(id),
    region text NOT NULL CHECK (region IN ('EU','US')),
    address text NOT NULL CHECK (char_length(address) BETWEEN 1 AND 300)
);

CREATE TABLE order_status_events (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    order_id uuid NOT NULL REFERENCES orders(id),
    actor_id uuid REFERENCES users(id),
    state text NOT NULL,
    reason text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX order_status_history_idx ON order_status_events (order_id, id);

CREATE TABLE quotes (
    order_id uuid PRIMARY KEY REFERENCES orders(id),
    provider_ref text NOT NULL UNIQUE,
    units bigint NOT NULL CHECK (units > 0),
    asset text NOT NULL CHECK (asset = 'TEST'),
    network text NOT NULL CHECK (network = 'simulation'),
    rate_numerator bigint NOT NULL CHECK (rate_numerator > 0),
    fee_units bigint NOT NULL CHECK (fee_units >= 0),
    expires_at timestamptz NOT NULL
);

CREATE TABLE payment_observations (
    event_key text PRIMARY KEY,
    order_id uuid NOT NULL REFERENCES orders(id),
    provider_ref text NOT NULL REFERENCES quotes(provider_ref),
    units bigint NOT NULL CHECK (units > 0),
    asset text NOT NULL,
    network text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE digital_entitlements (
    order_id uuid PRIMARY KEY REFERENCES orders(id),
    buyer_id uuid NOT NULL REFERENCES users(id),
    asset_id uuid NOT NULL REFERENCES digital_assets(id)
);

CREATE TABLE manual_deliveries (
    order_id uuid PRIMARY KEY REFERENCES orders(id),
    deadline timestamptz NOT NULL,
    payload text CHECK (char_length(payload) BETWEEN 1 AND 2000)
);

CREATE TABLE shipments (
    order_id uuid PRIMARY KEY REFERENCES orders(id),
    tracking_reference text CHECK (char_length(tracking_reference) BETWEEN 1 AND 100)
);

CREATE TABLE order_notifications (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id uuid NOT NULL REFERENCES orders(id),
    user_id uuid NOT NULL REFERENCES users(id),
    kind text NOT NULL CHECK (kind IN ('paid','shipment','download','manual_task','manual_ready','problem')),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (order_id, user_id, kind)
);
CREATE INDEX order_notifications_recipient_idx ON order_notifications (user_id, created_at DESC);

ALTER TABLE conversation_system_events ADD CONSTRAINT conversation_purchase_order_fk
    FOREIGN KEY (order_id) REFERENCES orders(id);

-- +goose StatementBegin
CREATE FUNCTION reject_order_history_change() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION '% records are append-only', TG_TABLE_NAME;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER order_items_immutable BEFORE UPDATE OR DELETE ON order_items
    FOR EACH ROW EXECUTE FUNCTION reject_order_history_change();
CREATE TRIGGER digital_assets_immutable BEFORE UPDATE OR DELETE ON digital_assets
    FOR EACH ROW EXECUTE FUNCTION reject_order_history_change();
CREATE TRIGGER payment_observations_immutable BEFORE UPDATE OR DELETE ON payment_observations
    FOR EACH ROW EXECUTE FUNCTION reject_order_history_change();
CREATE TRIGGER order_status_events_immutable BEFORE UPDATE OR DELETE ON order_status_events
    FOR EACH ROW EXECUTE FUNCTION reject_order_history_change();

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM orders) THEN
        RAISE EXCEPTION 'cannot roll back orders migration with order history; restore a backup or migrate forward';
    END IF;
END;
$$;
-- +goose StatementEnd
ALTER TABLE conversation_system_events DROP CONSTRAINT conversation_purchase_order_fk;
DROP TABLE order_notifications;
DROP TABLE shipments;
DROP TABLE manual_deliveries;
DROP TABLE digital_entitlements;
DROP TABLE payment_observations;
DROP TABLE quotes;
DROP TABLE order_status_events;
DROP TABLE shipping_details;
DROP TABLE order_items;
DROP TABLE orders;
ALTER TABLE listings DROP CONSTRAINT listing_digital_asset_fk;
ALTER TABLE listings DROP COLUMN digital_asset_id;
DROP TABLE digital_assets;
DROP FUNCTION reject_order_history_change();
