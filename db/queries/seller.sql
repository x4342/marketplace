-- name: CreateListingDraft :one
INSERT INTO listing_drafts (seller_id, fields, image_jpeg)
VALUES ($1, $2, $3)
RETURNING id;

-- name: SellerDraft :one
SELECT id, fields, (image_jpeg IS NOT NULL)::boolean AS has_image, revision
FROM listing_drafts
WHERE id = $1 AND seller_id = $2;

-- name: SellerDrafts :many
SELECT id, coalesce(fields->>'title', '')::text AS title, updated_at
FROM listing_drafts WHERE seller_id = $1 ORDER BY updated_at DESC, id;

-- name: SellerListings :many
SELECT id, title, status
FROM listings WHERE seller_id = $1 ORDER BY updated_at DESC, id;

-- name: UpdateListingDraft :execrows
UPDATE listing_drafts
SET fields = sqlc.arg(fields), image_jpeg = CASE
    WHEN sqlc.arg(remove_image)::boolean THEN NULL
    ELSE coalesce(sqlc.narg(image_jpeg)::bytea, image_jpeg)
    END,
    revision = revision + 1, updated_at = now()
WHERE id = sqlc.arg(id) AND seller_id = sqlc.arg(seller_id) AND revision = sqlc.arg(revision);

-- name: LockListingDraft :one
SELECT fields, image_jpeg, revision FROM listing_drafts
WHERE id = $1 AND seller_id = $2 FOR UPDATE;

-- name: PublishListing :execrows
INSERT INTO listings (id, seller_id, category_slug, title, description, kind, price_minor, currency,
                      stock, can_backorder, condition, ship_regions, delivery_mode, terms, status, published_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, 'published', now())
ON CONFLICT (id) DO UPDATE SET
    category_slug = excluded.category_slug,
    title = excluded.title,
    description = excluded.description,
    kind = excluded.kind,
    price_minor = excluded.price_minor,
    currency = excluded.currency,
    stock = excluded.stock,
    can_backorder = excluded.can_backorder,
    condition = excluded.condition,
    ship_regions = excluded.ship_regions,
    delivery_mode = excluded.delivery_mode,
    terms = excluded.terms,
    status = 'published',
    published_at = coalesce(listings.published_at, now()),
    updated_at = now()
WHERE listings.seller_id = excluded.seller_id AND listings.status <> 'removed';

-- name: ConsumeListingDraft :exec
DELETE FROM listing_drafts WHERE id = $1 AND seller_id = $2;

-- name: ChangeSellerListingStatus :execrows
UPDATE listings SET status = sqlc.arg(next_status), updated_at = now()
WHERE id = sqlc.arg(id) AND seller_id = sqlc.arg(seller_id)
  AND ((status = 'published' AND sqlc.arg(next_status)::text IN ('paused', 'removed'))
    OR (status = 'paused' AND sqlc.arg(next_status)::text IN ('published', 'removed')));

-- name: PutListingImage :exec
INSERT INTO listing_images (listing_id, image_jpeg) VALUES ($1, $2)
ON CONFLICT (listing_id) DO UPDATE SET image_jpeg = excluded.image_jpeg;

-- name: DeleteListingImage :exec
DELETE FROM listing_images WHERE listing_id = $1;

-- name: PublicListingImage :one
SELECT i.image_jpeg FROM listing_images i JOIN listings l ON l.id = i.listing_id
WHERE l.id = $1 AND l.status = 'published';

-- name: PrivateDraftImage :one
SELECT image_jpeg FROM listing_drafts WHERE id = $1 AND seller_id = $2 AND image_jpeg IS NOT NULL;

-- name: SellerVacation :one
SELECT EXISTS (SELECT 1 FROM seller_vacations WHERE seller_id = $1);

-- name: StartSellerVacation :exec
INSERT INTO seller_vacations (seller_id) VALUES ($1) ON CONFLICT DO NOTHING;

-- name: EndSellerVacation :exec
DELETE FROM seller_vacations WHERE seller_id = $1;

-- name: OpenListingDraft :execrows
INSERT INTO listing_drafts (id, seller_id, fields, image_jpeg)
SELECT l.id, l.seller_id, jsonb_build_object(
    'title', l.title, 'description', l.description, 'category', l.category_slug,
    'kind', l.kind, 'price', to_char(l.price_minor::numeric / 100, 'FM9999999990.00'),
    'currency', l.currency, 'stock', l.stock::text, 'orderable', l.can_backorder,
    'condition', l.condition, 'region_eu', 'EU' = ANY(l.ship_regions),
    'region_us', 'US' = ANY(l.ship_regions), 'delivery', l.delivery_mode, 'terms', l.terms
), i.image_jpeg
FROM listings l LEFT JOIN listing_images i ON i.listing_id = l.id
WHERE l.id = $1 AND l.seller_id = $2 AND l.status IN ('published', 'paused')
ON CONFLICT (id) DO NOTHING;
