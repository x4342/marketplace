-- +goose Up
ALTER TABLE role_assignments RENAME TO legacy_role_assignments;

CREATE TABLE role_assignments (
    user_id uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    role text NOT NULL CHECK (role IN ('user','moderator','admin'))
);

-- Both legacy assignments already granted the combined scope.
INSERT INTO role_assignments(user_id,role)
SELECT user_id,'moderator' FROM legacy_role_assignments
GROUP BY user_id HAVING count(*)=2;

-- +goose StatementBegin
CREATE FUNCTION marketplace_effective_role(account uuid) RETURNS text
STABLE LANGUAGE sql AS $$
    SELECT coalesce((SELECT role FROM role_assignments WHERE user_id=account),'user');
$$;
-- +goose StatementEnd

CREATE TABLE administrator_bootstrap (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    user_id uuid NOT NULL UNIQUE REFERENCES users(id),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE role_change_events (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    actor_id uuid NOT NULL REFERENCES users(id),
    subject_id uuid NOT NULL REFERENCES users(id),
    old_role text NOT NULL CHECK (old_role IN ('user','moderator','admin')),
    new_role text NOT NULL CHECK (new_role IN ('user','moderator','admin')),
    action text NOT NULL CHECK (action IN ('bootstrap','restore','operator_grant')),
    created_at timestamptz NOT NULL DEFAULT now()
);

-- +goose StatementBegin
CREATE FUNCTION reject_role_history_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'role history is immutable';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER immutable_legacy_roles BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE
ON legacy_role_assignments FOR EACH STATEMENT EXECUTE FUNCTION reject_role_history_change();
CREATE TRIGGER immutable_role_changes BEFORE UPDATE OR DELETE OR TRUNCATE
ON role_change_events FOR EACH STATEMENT EXECUTE FUNCTION reject_role_history_change();

ALTER TABLE security_events DROP CONSTRAINT security_events_action_check,
ADD CONSTRAINT security_events_action_check CHECK (action IN ('register','login','login_rejected','logout','recover','recovery_rejected','staff_reauth','staff_reauth_rejected'));

-- +goose Down
-- Never drop real role or audit data. This supports an empty schema round-trip.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM role_assignments)
       OR EXISTS (SELECT 1 FROM administrator_bootstrap)
       OR EXISTS (SELECT 1 FROM role_change_events)
       OR EXISTS (SELECT 1 FROM security_events WHERE action IN ('staff_reauth','staff_reauth_rejected')) THEN
        RAISE EXCEPTION 'cannot roll back populated effective roles or authorization history';
    END IF;
END;
$$;
-- +goose StatementEnd
DROP TRIGGER immutable_legacy_roles ON legacy_role_assignments;
DROP TRIGGER immutable_role_changes ON role_change_events;
DROP FUNCTION reject_role_history_change();
DROP TABLE role_change_events;
DROP TABLE administrator_bootstrap;
DROP FUNCTION marketplace_effective_role(uuid);
DROP TABLE role_assignments;
ALTER TABLE legacy_role_assignments RENAME TO role_assignments;
ALTER TABLE security_events DROP CONSTRAINT security_events_action_check,
ADD CONSTRAINT security_events_action_check CHECK (action IN ('register','login','login_rejected','logout','recover','recovery_rejected'));
