-- +goose Up
CREATE TABLE user_profiles (
    user_id uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    bio text NOT NULL DEFAULT '' CHECK (char_length(bio) <= 500),
    avatar_jpeg bytea CHECK (octet_length(avatar_jpeg) BETWEEN 1 AND 524287)
);

-- +goose Down
-- Fail closed if dropping the table would discard data.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM user_profiles) THEN
        RAISE EXCEPTION 'cannot roll back populated user profiles';
    END IF;
END;
$$;
-- +goose StatementEnd
DROP TABLE user_profiles;
