-- +goose Up
CREATE TABLE conversations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    listing_id uuid NOT NULL REFERENCES listings(id),
    buyer_id uuid NOT NULL REFERENCES users(id),
    seller_id uuid NOT NULL REFERENCES users(id),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (listing_id, buyer_id, seller_id),
    CHECK (buyer_id <> seller_id)
);
CREATE INDEX conversations_buyer_idx ON conversations (buyer_id, updated_at DESC);
CREATE INDEX conversations_seller_idx ON conversations (seller_id, updated_at DESC);

CREATE TABLE messages (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    sequence bigint NOT NULL CHECK (sequence > 0),
    conversation_id uuid NOT NULL REFERENCES conversations(id),
    sender_id uuid NOT NULL REFERENCES users(id),
    body text NOT NULL CHECK (char_length(body) BETWEEN 1 AND 2000),
    sent_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (conversation_id, sequence)
);

CREATE TABLE conversation_reads (
    conversation_id uuid NOT NULL REFERENCES conversations(id),
    user_id uuid NOT NULL REFERENCES users(id),
    last_sequence bigint NOT NULL CHECK (last_sequence >= 0),
    PRIMARY KEY (conversation_id, user_id)
);

CREATE TABLE conversation_blocks (
    conversation_id uuid NOT NULL REFERENCES conversations(id),
    user_id uuid NOT NULL REFERENCES users(id),
    PRIMARY KEY (conversation_id, user_id)
);

CREATE TABLE reports (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    reporter_id uuid NOT NULL REFERENCES users(id),
    conversation_id uuid NOT NULL REFERENCES conversations(id),
    reason text NOT NULL CHECK (char_length(reason) BETWEEN 1 AND 1000),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE notifications (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id),
    conversation_id uuid NOT NULL REFERENCES conversations(id),
    message_id uuid UNIQUE NOT NULL REFERENCES messages(id),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX notifications_user_idx ON notifications (user_id, created_at DESC, id);

-- M5 adds the orders foreign key and writes this table only on verified payment.
CREATE TABLE conversation_system_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id uuid NOT NULL REFERENCES conversations(id),
    order_id uuid UNIQUE NOT NULL,
    kind text NOT NULL CHECK (kind = 'purchase'),
    title_snapshot text NOT NULL CHECK (char_length(title_snapshot) BETWEEN 1 AND 140),
    created_at timestamptz NOT NULL DEFAULT now()
);

-- +goose StatementBegin
CREATE FUNCTION reject_conversation_event_change() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'conversation system events are immutable';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER conversation_system_events_immutable
BEFORE UPDATE OR DELETE ON conversation_system_events
FOR EACH ROW EXECUTE FUNCTION reject_conversation_event_change();

-- +goose Down
DROP TABLE conversation_system_events;
DROP FUNCTION reject_conversation_event_change();
DROP TABLE notifications;
DROP TABLE reports;
DROP TABLE conversation_blocks;
DROP TABLE conversation_reads;
DROP TABLE messages;
DROP TABLE conversations;
