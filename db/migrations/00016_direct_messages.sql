-- +goose Up
ALTER TABLE conversations ALTER COLUMN listing_id DROP NOT NULL,
    ADD COLUMN direct_opened_by uuid REFERENCES users(id),
    ADD COLUMN direct_opened_at timestamptz,
    ADD CONSTRAINT conversation_context CHECK (
        (listing_id IS NOT NULL AND direct_opened_by IS NULL AND direct_opened_at IS NULL)
        OR (listing_id IS NULL AND buyer_id < seller_id AND direct_opened_by IS NOT NULL
            AND direct_opened_by IN (buyer_id,seller_id) AND direct_opened_at IS NOT NULL)
    );
CREATE UNIQUE INDEX conversations_direct_pair_idx ON conversations (buyer_id,seller_id) WHERE listing_id IS NULL;
CREATE INDEX conversations_direct_open_rate_idx ON conversations (direct_opened_by,direct_opened_at DESC) WHERE listing_id IS NULL;
CREATE INDEX messages_sender_rate_idx ON messages (sender_id,sent_at DESC);

-- +goose StatementBegin
CREATE FUNCTION require_purchase_conversation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM conversations c JOIN orders o ON o.id=NEW.order_id
        JOIN order_items i ON i.order_id=o.id
        WHERE c.id=NEW.conversation_id AND c.listing_id=o.listing_id
          AND c.buyer_id=o.buyer_id AND c.seller_id=o.seller_id AND i.title=NEW.title_snapshot
          AND EXISTS (SELECT 1 FROM payment_observations p WHERE p.order_id=o.id)
    ) THEN
        RAISE check_violation USING CONSTRAINT = 'conversation_verified_purchase';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER conversation_purchase_verified BEFORE INSERT ON conversation_system_events
FOR EACH ROW EXECUTE FUNCTION require_purchase_conversation();

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM conversations WHERE listing_id IS NULL) THEN
        RAISE EXCEPTION 'restore a pre-upgrade archive to roll back populated direct messaging';
    END IF;
END;
$$;
-- +goose StatementEnd
DROP TRIGGER conversation_purchase_verified ON conversation_system_events;
DROP FUNCTION require_purchase_conversation();
DROP INDEX messages_sender_rate_idx;
DROP INDEX conversations_direct_open_rate_idx;
DROP INDEX conversations_direct_pair_idx;
ALTER TABLE conversations DROP CONSTRAINT conversation_context,
    DROP COLUMN direct_opened_by, DROP COLUMN direct_opened_at,
    ALTER COLUMN listing_id SET NOT NULL;
