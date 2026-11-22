-- +goose Up
CREATE TABLE categories (
    slug text PRIMARY KEY CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
    name text NOT NULL,
    parent_slug text REFERENCES categories(slug)
);

CREATE TABLE listings (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    seller_id uuid NOT NULL REFERENCES users(id),
    category_slug text NOT NULL REFERENCES categories(slug),
    title text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 140),
    description text NOT NULL CHECK (char_length(description) BETWEEN 1 AND 3000),
    kind text NOT NULL CHECK (kind IN ('physical', 'digital')),
    price_minor bigint NOT NULL CHECK (price_minor BETWEEN 1 AND 100000000000),
    currency text NOT NULL CHECK (currency IN ('USD', 'EUR')),
    stock integer NOT NULL CHECK (stock >= 0),
    can_backorder boolean NOT NULL DEFAULT false,
    condition text NOT NULL DEFAULT '',
    ship_regions text[] NOT NULL DEFAULT '{}',
    delivery_mode text NOT NULL DEFAULT '',
    terms text NOT NULL,
    status text NOT NULL CHECK (status IN ('draft', 'published', 'paused', 'removed')),
    created_at timestamptz NOT NULL DEFAULT now(),
    published_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now(),
    search_document tsvector GENERATED ALWAYS AS (
        setweight(to_tsvector('simple', title), 'A') ||
        setweight(to_tsvector('simple', description), 'B')
    ) STORED,
    CHECK (status <> 'published' OR published_at IS NOT NULL),
    CHECK (
        (kind = 'physical' AND condition IN ('new', 'used', 'refurbished') AND
            cardinality(ship_regions) > 0 AND delivery_mode = '') OR
        (kind = 'digital' AND delivery_mode IN ('automatic', 'manual') AND
            condition = '' AND cardinality(ship_regions) = 0)
    )
);

CREATE INDEX listings_search_idx ON listings USING gin (search_document) WHERE status = 'published';
CREATE INDEX listings_published_idx ON listings (published_at DESC, id) WHERE status = 'published';
CREATE INDEX listings_seller_idx ON listings (seller_id) WHERE status = 'published';
CREATE INDEX listings_facets_idx ON listings (category_slug, kind, currency, price_minor) WHERE status = 'published';

CREATE TABLE favorites (
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    listing_id uuid NOT NULL REFERENCES listings(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, listing_id)
);

CREATE TABLE catalog_fx_rate (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    usd_per_eur_ppm bigint NOT NULL CHECK (usd_per_eur_ppm BETWEEN 100000 AND 10000000),
    rate_date date NOT NULL
);

INSERT INTO catalog_fx_rate (usd_per_eur_ppm, rate_date) VALUES (1100000, '2026-11-22');

-- +goose Down
DROP TABLE catalog_fx_rate;
DROP TABLE favorites;
DROP TABLE listings;
DROP TABLE categories;
