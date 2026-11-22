-- +goose Up
CREATE TABLE listing_drafts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    seller_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    fields jsonb NOT NULL CHECK (jsonb_typeof(fields) = 'object'),
    image_jpeg bytea CHECK (octet_length(image_jpeg) BETWEEN 1 AND 3145728),
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX listing_drafts_seller_idx ON listing_drafts (seller_id, updated_at DESC);

CREATE TABLE listing_images (
    listing_id uuid PRIMARY KEY REFERENCES listings(id) ON DELETE CASCADE,
    image_jpeg bytea NOT NULL CHECK (octet_length(image_jpeg) BETWEEN 1 AND 3145728)
);

CREATE TABLE seller_vacations (
    seller_id uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    started_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE seller_vacations;
DROP TABLE listing_images;
DROP TABLE listing_drafts;
