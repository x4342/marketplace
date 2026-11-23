-- +goose Up
ALTER TABLE categories
    ADD COLUMN id uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    ADD COLUMN description text NOT NULL DEFAULT '' CHECK (char_length(description) <= 1000),
    ADD COLUMN display_order integer NOT NULL DEFAULT 0 CHECK (display_order BETWEEN 0 AND 9999),
    ADD COLUMN active boolean NOT NULL DEFAULT true,
    ADD COLUMN revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0);

ALTER TABLE categories DROP CONSTRAINT categories_parent_slug_fkey,
    ADD FOREIGN KEY (parent_slug) REFERENCES categories(slug) ON UPDATE CASCADE;
ALTER TABLE listings DROP CONSTRAINT listings_category_slug_fkey,
    ADD FOREIGN KEY (category_slug) REFERENCES categories(slug) ON UPDATE CASCADE;
CREATE INDEX listings_category_usage_idx ON listings(category_slug);
CREATE INDEX categories_parent_idx ON categories(parent_slug);

CREATE TABLE category_slug_history (
    slug text PRIMARY KEY,
    category_id uuid NOT NULL REFERENCES categories(id)
);
CREATE TABLE category_change_events (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    actor_id uuid NOT NULL REFERENCES users(id),
    category_id uuid NOT NULL REFERENCES categories(id),
    before_state jsonb,
    after_state jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- +goose StatementBegin
CREATE FUNCTION serialize_category_write() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(5194898321094018);
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION protect_category_graph() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE' AND NEW.id <> OLD.id THEN
        RAISE check_violation USING CONSTRAINT = 'category_immutable_id';
    END IF;
    IF EXISTS (SELECT 1 FROM category_slug_history WHERE slug=NEW.slug AND category_id<>NEW.id) THEN
        RAISE unique_violation USING CONSTRAINT = 'category_slug_reserved';
    END IF;
    IF NEW.parent_slug IS NOT NULL AND (
        NEW.parent_slug = NEW.slug
        OR (TG_OP = 'UPDATE' AND NEW.parent_slug = OLD.slug)
        OR EXISTS (SELECT 1 FROM categories p WHERE p.slug=NEW.parent_slug AND p.parent_slug IS NOT NULL)
        OR EXISTS (SELECT 1 FROM categories c WHERE c.parent_slug=NEW.slug OR (TG_OP = 'UPDATE' AND c.parent_slug=OLD.slug))
    ) THEN
        RAISE check_violation USING CONSTRAINT = 'category_two_levels';
    END IF;
    IF TG_OP = 'UPDATE' AND NEW.slug <> OLD.slug THEN
        INSERT INTO category_slug_history(slug,category_id) VALUES (OLD.slug,OLD.id)
        ON CONFLICT (slug) DO NOTHING;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER serialize_category_write BEFORE INSERT OR UPDATE OR DELETE ON categories
FOR EACH STATEMENT EXECUTE FUNCTION serialize_category_write();
CREATE TRIGGER protect_category_graph BEFORE INSERT OR UPDATE ON categories
FOR EACH ROW EXECUTE FUNCTION protect_category_graph();
CREATE TRIGGER immutable_category_events BEFORE UPDATE OR DELETE OR TRUNCATE ON category_change_events
FOR EACH STATEMENT EXECUTE FUNCTION reject_role_history_change();
CREATE TRIGGER immutable_category_slug_history BEFORE UPDATE OR DELETE OR TRUNCATE ON category_slug_history
FOR EACH STATEMENT EXECUTE FUNCTION reject_role_history_change();

ALTER TABLE role_change_events DROP CONSTRAINT role_change_events_action_check,
ADD CONSTRAINT role_change_events_action_check
CHECK (action IN ('bootstrap','restore','operator_grant','dashboard_assignment'));

-- +goose Down
-- Populated M13 installations use archive restoration to a separate volume.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM category_change_events)
       OR EXISTS (SELECT 1 FROM category_slug_history)
       OR EXISTS (SELECT 1 FROM categories WHERE description <> '' OR display_order <> 0 OR NOT active OR revision <> 1)
       OR EXISTS (SELECT 1 FROM role_change_events WHERE action='dashboard_assignment') THEN
        RAISE EXCEPTION 'cannot discard populated M13 administration state';
    END IF;
END;
$$;
-- +goose StatementEnd
ALTER TABLE role_change_events DROP CONSTRAINT role_change_events_action_check,
ADD CONSTRAINT role_change_events_action_check CHECK (action IN ('bootstrap','restore','operator_grant'));
DROP TRIGGER protect_category_graph ON categories;
DROP TRIGGER serialize_category_write ON categories;
DROP FUNCTION protect_category_graph();
DROP FUNCTION serialize_category_write();
DROP TABLE category_change_events;
DROP TABLE category_slug_history;
ALTER TABLE categories DROP CONSTRAINT categories_parent_slug_fkey,
    ADD FOREIGN KEY (parent_slug) REFERENCES categories(slug);
ALTER TABLE listings DROP CONSTRAINT listings_category_slug_fkey,
    ADD FOREIGN KEY (category_slug) REFERENCES categories(slug);
DROP INDEX listings_category_usage_idx;
DROP INDEX categories_parent_idx;
ALTER TABLE categories DROP COLUMN revision, DROP COLUMN active,
    DROP COLUMN display_order, DROP COLUMN description, DROP COLUMN id;
