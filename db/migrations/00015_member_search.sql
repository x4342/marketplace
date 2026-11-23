-- +goose Up
CREATE INDEX users_username_prefix_idx ON users (username COLLATE "C");

-- +goose Down
DROP INDEX users_username_prefix_idx;
