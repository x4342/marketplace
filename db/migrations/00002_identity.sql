-- +goose Up
CREATE TABLE users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    username text NOT NULL UNIQUE CHECK (username ~ '^[a-z0-9_]{3,30}$'),
    password_hash bytea NOT NULL CHECK (octet_length(password_hash) = 49),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
    token_hash bytea PRIMARY KEY CHECK (octet_length(token_hash) = 32),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (expires_at > created_at)
);

CREATE INDEX sessions_user_id_idx ON sessions(user_id);
CREATE INDEX sessions_expires_at_idx ON sessions(expires_at);

CREATE TABLE recovery_codes (
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash bytea NOT NULL CHECK (octet_length(token_hash) = 32),
    PRIMARY KEY (user_id, token_hash)
);

CREATE TABLE role_assignments (
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role text NOT NULL CHECK (role IN ('moderator', 'finance_admin')),
    PRIMARY KEY (user_id, role)
);

CREATE TABLE security_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid REFERENCES users(id) ON DELETE SET NULL,
    action text NOT NULL CHECK (action IN ('register', 'login', 'login_rejected', 'logout', 'recover', 'recovery_rejected')),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX security_events_user_id_idx ON security_events(user_id, created_at DESC);

-- +goose Down
DROP TABLE security_events;
DROP TABLE role_assignments;
DROP TABLE recovery_codes;
DROP TABLE sessions;
DROP TABLE users;
