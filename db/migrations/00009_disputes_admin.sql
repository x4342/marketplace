-- +goose Up
CREATE TABLE admin_audit_events (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    actor_id uuid NOT NULL REFERENCES users(id),
    area text NOT NULL CHECK (area IN ('reviews','payouts','payment','dispute','content')),
    subject_id uuid NOT NULL,
    action text NOT NULL,
    reason text NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 1 AND 1400),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX admin_audit_recent_idx ON admin_audit_events(area, id DESC);
CREATE TRIGGER admin_audit_immutable BEFORE UPDATE OR DELETE ON admin_audit_events
    FOR EACH ROW EXECUTE FUNCTION reject_order_history_change();

CREATE TABLE disputes (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id uuid NOT NULL UNIQUE REFERENCES orders(id),
    status text NOT NULL DEFAULT 'open' CHECK (status IN ('open','hold','released','refunded')),
    category text NOT NULL CHECK (category IN ('not_received','not_as_described','delivery_failed','other')),
    decided_by uuid REFERENCES users(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX disputes_queue_idx ON disputes(status,updated_at,id);

CREATE TABLE dispute_events (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    dispute_id uuid NOT NULL REFERENCES disputes(id),
    actor_id uuid REFERENCES users(id),
    action text NOT NULL,
    detail text NOT NULL CHECK(char_length(detail) BETWEEN 1 AND 2000),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX dispute_events_dispute_idx ON dispute_events(dispute_id,id);
CREATE TRIGGER dispute_history_immutable BEFORE UPDATE OR DELETE ON dispute_events
    FOR EACH ROW EXECUTE FUNCTION reject_order_history_change();

CREATE TABLE case_evidence (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    dispute_id uuid NOT NULL REFERENCES disputes(id),
    actor_id uuid NOT NULL REFERENCES users(id),
    kind text NOT NULL CHECK(kind IN ('response','evidence')),
    body text NOT NULL CHECK(char_length(btrim(body)) BETWEEN 1 AND 2000),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX case_evidence_dispute_idx ON case_evidence(dispute_id,created_at,id);
CREATE TRIGGER case_evidence_immutable BEFORE UPDATE OR DELETE ON case_evidence
    FOR EACH ROW EXECUTE FUNCTION reject_order_history_change();

CREATE TABLE dispute_appeals (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    dispute_id uuid NOT NULL REFERENCES disputes(id),
    actor_id uuid NOT NULL REFERENCES users(id),
    reason text NOT NULL CHECK(char_length(btrim(reason)) BETWEEN 1 AND 1000),
    outcome text NOT NULL DEFAULT 'pending' CHECK(outcome IN ('pending','upheld','referred')),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(dispute_id,actor_id)
);

CREATE TABLE dispute_notices (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users(id),
    dispute_id uuid NOT NULL REFERENCES disputes(id),
    label text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX dispute_notices_user_idx ON dispute_notices(user_id,id DESC);

ALTER TABLE seller_payouts ADD COLUMN hold_active boolean NOT NULL DEFAULT false;
ALTER TABLE conversations ADD COLUMN moderation_blocked boolean NOT NULL DEFAULT false;
ALTER TABLE listings ADD COLUMN moderation_locked boolean NOT NULL DEFAULT false;

CREATE TABLE user_restrictions (
    user_id uuid PRIMARY KEY REFERENCES users(id),
    moderator_id uuid NOT NULL REFERENCES users(id),
    reason text NOT NULL CHECK(char_length(btrim(reason)) BETWEEN 1 AND 1000),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE content_reports (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    reporter_id uuid NOT NULL REFERENCES users(id),
    listing_id uuid REFERENCES listings(id),
    subject_user_id uuid REFERENCES users(id),
    conversation_id uuid REFERENCES conversations(id),
    legacy_report_id uuid UNIQUE REFERENCES reports(id),
    reason text NOT NULL CHECK(char_length(btrim(reason)) BETWEEN 1 AND 1000),
    state text NOT NULL DEFAULT 'open' CHECK(state IN ('open','dismissed','restricted','restored','upheld')),
    decided_by uuid REFERENCES users(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK(num_nonnulls(listing_id,subject_user_id,conversation_id)=1),
    UNIQUE NULLS NOT DISTINCT(reporter_id,listing_id,subject_user_id,conversation_id)
);
CREATE INDEX content_report_queue_idx ON content_reports(state,created_at,id);

CREATE TABLE content_appeals (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    report_id uuid NOT NULL REFERENCES content_reports(id),
    actor_id uuid NOT NULL REFERENCES users(id),
    reason text NOT NULL CHECK(char_length(btrim(reason)) BETWEEN 1 AND 1000),
    resolved_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(report_id,actor_id)
);

-- +goose StatementBegin
CREATE FUNCTION queue_chat_report() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO content_reports(reporter_id,conversation_id,legacy_report_id,reason)
    VALUES(NEW.reporter_id,NEW.conversation_id,NEW.id,NEW.reason) ON CONFLICT DO NOTHING;
    RETURN NEW;
END;
$$;
CREATE FUNCTION audit_existing_admin_action() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_TABLE_NAME='review_moderation_actions' THEN
        INSERT INTO admin_audit_events(actor_id,area,subject_id,action,reason)
        VALUES(NEW.moderator_id,'reviews',NEW.review_id,NEW.action,NEW.reason);
    ELSIF TG_TABLE_NAME='payout_reviews' AND NEW.action<>'reserved' THEN
        INSERT INTO admin_audit_events(actor_id,area,subject_id,action,reason)
        VALUES(NEW.actor_id,'payouts',NEW.payout_id,NEW.action,NEW.reason);
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER report_moderation_queue AFTER INSERT ON reports
    FOR EACH ROW EXECUTE FUNCTION queue_chat_report();
CREATE TRIGGER audit_review_to_admin AFTER INSERT ON review_moderation_actions
    FOR EACH ROW EXECUTE FUNCTION audit_existing_admin_action();
CREATE TRIGGER audit_payout_to_admin AFTER INSERT ON payout_reviews
    FOR EACH ROW EXECUTE FUNCTION audit_existing_admin_action();

INSERT INTO content_reports(reporter_id,conversation_id,legacy_report_id,reason,created_at)
SELECT reporter_id,conversation_id,id,reason,created_at FROM reports WHERE btrim(reason)<>''
ORDER BY created_at,id ON CONFLICT DO NOTHING;
INSERT INTO admin_audit_events(actor_id,area,subject_id,action,reason,created_at)
SELECT moderator_id,'reviews',review_id,action,reason,created_at FROM review_moderation_actions
UNION ALL
SELECT actor_id,'payouts',payout_id,action,reason,created_at FROM payout_reviews WHERE action<>'reserved'
UNION ALL
SELECT actor_id,'payment',order_id,CASE WHEN reason LIKE 'Simulated chain reversal%' THEN 'reversal' ELSE 'refund' END,reason,created_at
FROM order_status_events WHERE actor_id IS NOT NULL AND state='refunded'
AND (reason LIKE 'Simulated chain reversal%' OR reason LIKE 'Simulated full refund%');
INSERT INTO disputes(order_id,category)
SELECT id,'other' FROM orders WHERE state='disputed';

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS(SELECT 1 FROM disputes) OR EXISTS(SELECT 1 FROM content_reports)
        OR EXISTS(SELECT 1 FROM admin_audit_events) OR EXISTS(SELECT 1 FROM user_restrictions)
    THEN RAISE EXCEPTION 'cannot roll back populated dispute or moderation history'; END IF;
END;
$$;
-- +goose StatementEnd
DROP TRIGGER audit_payout_to_admin ON payout_reviews;
DROP TRIGGER audit_review_to_admin ON review_moderation_actions;
DROP TRIGGER report_moderation_queue ON reports;
DROP FUNCTION audit_existing_admin_action();
DROP FUNCTION queue_chat_report();
DROP TABLE content_appeals;
DROP TABLE content_reports;
DROP TABLE user_restrictions;
ALTER TABLE listings DROP COLUMN moderation_locked;
ALTER TABLE seller_payouts DROP COLUMN hold_active;
ALTER TABLE conversations DROP COLUMN moderation_blocked;
DROP TABLE dispute_notices;
DROP TABLE dispute_appeals;
DROP TABLE case_evidence;
DROP TABLE dispute_events;
DROP TABLE disputes;
DROP TABLE admin_audit_events;
