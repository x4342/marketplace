-- +goose Up
CREATE TABLE worker_heartbeat (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    observed_at timestamptz NOT NULL
);

-- +goose Down
DROP TABLE worker_heartbeat;
