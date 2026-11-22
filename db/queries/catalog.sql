-- name: CatalogCategories :many
SELECT slug, name, coalesce(parent_slug, '')::text AS parent
FROM categories ORDER BY coalesce(parent_slug, slug), parent_slug NULLS FIRST, name;

-- name: DiscoverListings :many
WITH seller_ratings AS (
    SELECT seller_id, count(*) AS review_count, avg(stars)::float8 AS rating
    FROM reviews WHERE NOT hidden GROUP BY seller_id
), completed_sales AS (
    SELECT o.listing_id,count(*) AS purchases
    FROM orders o WHERE o.state='completed'
      AND EXISTS (SELECT 1 FROM payment_observations p WHERE p.order_id=o.id)
    GROUP BY o.listing_id
), candidates AS (
    SELECT l.id, l.title, l.kind, l.price_minor, l.currency, l.stock, l.can_backorder, l.published_at,
           c.name AS category_name, u.username AS seller_name,
           coalesce(sr.review_count,0)::bigint AS review_count,
           coalesce(sr.rating,0)::float8 AS seller_rating,
           coalesce(cs.purchases,0)::bigint AS purchases,
           EXISTS (SELECT 1 FROM listing_images i WHERE i.listing_id = l.id) AS has_image,
           EXISTS (SELECT 1 FROM seller_vacations v WHERE v.seller_id = l.seller_id) AS seller_away,
           EXISTS (SELECT 1 FROM favorites f WHERE f.listing_id = l.id AND f.user_id = sqlc.arg(viewer_id)::uuid) AS favorite,
           CASE
               WHEN l.currency = sqlc.arg(display_currency) THEN l.price_minor
               WHEN l.currency = 'EUR' THEN (l.price_minor * fx.usd_per_eur_ppm + 500000) / 1000000
               ELSE (l.price_minor * 1000000 + fx.usd_per_eur_ppm / 2) / fx.usd_per_eur_ppm
           END AS comparable_minor,
           ts_rank(l.search_document, websearch_to_tsquery('simple', sqlc.arg(keyword)::text)) AS relevance_score
    FROM listings l
    JOIN categories c ON c.slug = l.category_slug
    JOIN users u ON u.id = l.seller_id
    CROSS JOIN catalog_fx_rate fx
    LEFT JOIN seller_ratings sr ON sr.seller_id=l.seller_id
    LEFT JOIN completed_sales cs ON cs.listing_id=l.id
    WHERE l.status = 'published'
      AND (NOT sqlc.arg(reviewed)::boolean OR EXISTS (SELECT 1 FROM reviews r WHERE r.listing_id=l.id AND NOT r.hidden))
      AND (sqlc.arg(min_rating)::smallint=0 OR (sr.review_count >= sqlc.arg(rating_sample)::bigint AND sr.rating >= sqlc.arg(min_rating)::smallint))
      AND (NOT sqlc.arg(saved_only)::boolean OR EXISTS (
          SELECT 1 FROM favorites f WHERE f.listing_id = l.id AND f.user_id = sqlc.arg(viewer_id)::uuid
      ))
      AND (sqlc.arg(keyword)::text = '' OR l.search_document @@ websearch_to_tsquery('simple', sqlc.arg(keyword)::text))
      AND (sqlc.arg(kind)::text = '' OR l.kind = sqlc.arg(kind)::text)
      AND (sqlc.arg(category)::text = '' OR c.slug = sqlc.arg(category)::text OR c.parent_slug = sqlc.arg(category)::text)
      AND (sqlc.arg(seller)::text = '' OR u.username = sqlc.arg(seller)::text)
      AND (sqlc.arg(condition)::text = '' OR l.condition = sqlc.arg(condition)::text)
      AND (sqlc.arg(region)::text = '' OR sqlc.arg(region)::text = ANY(l.ship_regions))
      AND (sqlc.arg(delivery)::text = '' OR l.delivery_mode = sqlc.arg(delivery)::text)
      AND (sqlc.arg(availability)::text = ''
           OR (sqlc.arg(availability)::text = 'in_stock' AND l.stock > 0)
           OR (sqlc.arg(availability)::text = 'orderable' AND l.can_backorder))
)
SELECT id, title, kind, price_minor, currency, stock, can_backorder, category_name, seller_name, favorite, has_image, seller_away, review_count, seller_rating,
       count(*) OVER () AS total_count
FROM candidates
WHERE comparable_minor BETWEEN sqlc.arg(minimum)::bigint AND sqlc.arg(maximum)::bigint
ORDER BY
    CASE WHEN sqlc.arg(sort_mode)::text = 'popular' AND purchases >= sqlc.arg(rating_sample)::bigint THEN purchases END DESC NULLS LAST,
    CASE WHEN sqlc.arg(sort_mode)::text = 'rating' AND review_count >= sqlc.arg(rating_sample)::bigint THEN seller_rating END DESC NULLS LAST,
    CASE WHEN sqlc.arg(sort_mode)::text = 'relevance' THEN relevance_score END DESC,
    CASE WHEN sqlc.arg(sort_mode)::text = 'price_asc' THEN comparable_minor END ASC,
    CASE WHEN sqlc.arg(sort_mode)::text = 'price_desc' THEN comparable_minor END DESC,
    published_at DESC, id
LIMIT 12 OFFSET sqlc.arg(skip_count)::integer;

-- name: CatalogFX :one
SELECT usd_per_eur_ppm, rate_date FROM catalog_fx_rate WHERE singleton;

-- name: PublicListing :one
SELECT l.id, l.seller_id, l.digital_asset_id, l.title, l.description, l.kind, l.price_minor, l.currency, l.stock, l.can_backorder,
       l.condition, l.ship_regions, l.delivery_mode, l.terms, c.name AS category_name, u.username AS seller_name,
       EXISTS (SELECT 1 FROM listing_images i WHERE i.listing_id = l.id) AS has_image,
       EXISTS (SELECT 1 FROM seller_vacations v WHERE v.seller_id = l.seller_id) AS seller_away
FROM listings l
JOIN categories c ON c.slug = l.category_slug
JOIN users u ON u.id = l.seller_id
WHERE l.id = $1 AND l.status = 'published';

-- name: PublicStorefront :one
SELECT u.id,u.username,u.created_at,
       EXISTS (SELECT 1 FROM seller_vacations v WHERE v.seller_id=u.id) AS away
FROM users u
WHERE u.username = $1 AND EXISTS (
    SELECT 1 FROM listings l WHERE l.seller_id = u.id AND l.status = 'published'
);

-- name: HasFavorite :one
SELECT EXISTS (SELECT 1 FROM favorites WHERE user_id = $1 AND listing_id = $2);

-- name: SaveFavorite :exec
INSERT INTO favorites (user_id, listing_id)
SELECT $1, id FROM listings WHERE id = $2 AND status = 'published'
ON CONFLICT (user_id, listing_id) DO NOTHING;

-- name: RemoveFavorite :exec
DELETE FROM favorites WHERE user_id = $1 AND listing_id = $2;

-- name: CatalogHasPopularListing :one
SELECT EXISTS (
    SELECT 1 FROM orders o JOIN listings l ON l.id=o.listing_id
    WHERE o.state='completed' AND l.status='published'
    AND EXISTS (SELECT 1 FROM payment_observations p WHERE p.order_id=o.id)
    GROUP BY o.listing_id HAVING count(*) >= sqlc.arg(minimum)::bigint
)::boolean;
