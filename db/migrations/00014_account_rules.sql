-- +goose Up
-- Existing rows keep their usernames and their original unique constraint.
-- +goose StatementBegin
CREATE FUNCTION protect_new_username() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE' AND NEW.username = OLD.username THEN
        RETURN NEW;
    END IF;
    IF NEW.username COLLATE "C" !~ '^[a-z0-9]{3,20}$' THEN
        RAISE check_violation USING CONSTRAINT = 'users_new_username_policy';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER users_new_username_policy BEFORE INSERT OR UPDATE OF username ON users
FOR EACH ROW EXECUTE FUNCTION protect_new_username();

-- +goose Down
DROP TRIGGER users_new_username_policy ON users;
DROP FUNCTION protect_new_username();
