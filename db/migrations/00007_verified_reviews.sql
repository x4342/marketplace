-- +goose Up
CREATE TABLE reviews (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id uuid NOT NULL UNIQUE REFERENCES orders(id),
    author_id uuid NOT NULL REFERENCES users(id),
    listing_id uuid NOT NULL REFERENCES listings(id),
    seller_id uuid NOT NULL REFERENCES users(id),
    stars smallint NOT NULL CHECK (stars BETWEEN 1 AND 5),
    body text NOT NULL CHECK (char_length(btrim(body)) BETWEEN 1 AND 2000),
    hidden boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (author_id <> seller_id)
);
CREATE INDEX reviews_public_listing_idx ON reviews(listing_id, created_at DESC, id) WHERE NOT hidden;
CREATE INDEX reviews_public_seller_idx ON reviews(seller_id) WHERE NOT hidden;

-- +goose StatementBegin
CREATE FUNCTION enforce_verified_review() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM orders o WHERE o.id = NEW.order_id AND o.state = 'completed'
          AND o.buyer_id = NEW.author_id AND o.seller_id = NEW.seller_id AND o.listing_id = NEW.listing_id
          AND EXISTS (SELECT 1 FROM payment_observations p WHERE p.order_id = o.id)
        FOR UPDATE OF o
    ) THEN
        RAISE EXCEPTION 'review requires a completed verified purchase' USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;
CREATE FUNCTION preserve_review_content() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        IF (to_jsonb(NEW) - 'hidden') = (to_jsonb(OLD) - 'hidden') THEN
            RETURN NEW;
        END IF;
    END IF;
    RAISE EXCEPTION 'review history is immutable';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER reviews_verified BEFORE INSERT ON reviews
    FOR EACH ROW EXECUTE FUNCTION enforce_verified_review();
CREATE TRIGGER reviews_preserve_content BEFORE UPDATE OR DELETE ON reviews
    FOR EACH ROW EXECUTE FUNCTION preserve_review_content();

CREATE TABLE review_reports (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    review_id uuid NOT NULL REFERENCES reviews(id),
    reporter_id uuid NOT NULL REFERENCES users(id),
    reason text NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 1 AND 1000),
    created_at timestamptz NOT NULL DEFAULT now(),
    resolved_at timestamptz,
    UNIQUE(review_id, reporter_id)
);
CREATE INDEX review_reports_open_idx ON review_reports(created_at, id) WHERE resolved_at IS NULL;

CREATE TABLE review_moderation_actions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    review_id uuid NOT NULL REFERENCES reviews(id),
    moderator_id uuid NOT NULL REFERENCES users(id),
    action text NOT NULL CHECK (action IN ('dismiss','hide','uphold','restore')),
    reason text NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 1 AND 1000),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER review_actions_immutable BEFORE UPDATE OR DELETE ON review_moderation_actions
    FOR EACH ROW EXECUTE FUNCTION reject_order_history_change();

CREATE TABLE review_appeals (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    review_id uuid NOT NULL REFERENCES reviews(id),
    hide_action_id uuid NOT NULL UNIQUE REFERENCES review_moderation_actions(id),
    appellant_id uuid NOT NULL REFERENCES users(id),
    reason text NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 1 AND 1000),
    created_at timestamptz NOT NULL DEFAULT now(),
    resolved_at timestamptz
);
CREATE UNIQUE INDEX review_appeals_open_idx ON review_appeals(review_id) WHERE resolved_at IS NULL;

CREATE TABLE review_notifications (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id),
    review_id uuid NOT NULL REFERENCES reviews(id),
    kind text NOT NULL CHECK (kind IN ('received','hidden','upheld','restored')),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX review_notifications_recipient_idx ON review_notifications(user_id, created_at DESC, id);

CREATE TABLE listing_view_counts (
    listing_id uuid PRIMARY KEY REFERENCES listings(id),
    views bigint NOT NULL CHECK (views > 0)
);

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM reviews) THEN
        RAISE EXCEPTION 'cannot roll back with verified review history';
    END IF;
END;
$$;
-- +goose StatementEnd
DROP TABLE listing_view_counts;
DROP TABLE review_notifications;
DROP TABLE review_appeals;
DROP TABLE review_moderation_actions;
DROP TABLE review_reports;
DROP TABLE reviews;
DROP FUNCTION preserve_review_content();
DROP FUNCTION enforce_verified_review();
