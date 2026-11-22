-- name: BuyerReviewStatus :one
SELECT (o.state = 'completed' AND o.buyer_id <> o.seller_id
        AND EXISTS (SELECT 1 FROM payment_observations p WHERE p.order_id = o.id))::boolean AS eligible,
       r.id AS review_id
FROM orders o LEFT JOIN reviews r ON r.order_id = o.id
WHERE o.id = sqlc.arg(order_id) AND o.buyer_id = sqlc.arg(actor)::uuid;

-- name: CreateVerifiedReview :one
INSERT INTO reviews (order_id,author_id,listing_id,seller_id,stars,body)
SELECT o.id,o.buyer_id,o.listing_id,o.seller_id,sqlc.arg(stars)::smallint,sqlc.arg(body)::text
FROM orders o WHERE o.id = sqlc.arg(order_id) AND o.buyer_id = sqlc.arg(actor)::uuid
AND o.state = 'completed' AND o.buyer_id <> o.seller_id
AND EXISTS (SELECT 1 FROM payment_observations p WHERE p.order_id = o.id)
ON CONFLICT (order_id) DO NOTHING
RETURNING id,seller_id;

-- name: ListingReviews :many
SELECT r.id,r.stars,r.body,r.created_at,i.title AS product,o.state='refunded' AS refunded
FROM reviews r JOIN orders o ON o.id=r.order_id JOIN order_items i ON i.order_id=o.id
WHERE r.listing_id=sqlc.arg(listing_id) AND NOT r.hidden
ORDER BY r.created_at DESC,r.id LIMIT 21 OFFSET sqlc.arg(page_offset)::integer;

-- name: ListingReviewCounts :many
SELECT stars,count(*) AS total FROM reviews WHERE listing_id=$1 AND NOT hidden GROUP BY stars ORDER BY stars DESC;

-- name: SellerRating :one
SELECT count(*) AS review_count, coalesce(round(avg(stars),1),0)::float8 AS average
FROM reviews WHERE seller_id=$1 AND NOT hidden;

-- name: LockReview :one
SELECT id,listing_id,author_id,seller_id,hidden FROM reviews WHERE id=$1 FOR UPDATE;

-- name: PrivateReview :one
SELECT r.id,r.author_id,r.stars,r.body,r.hidden,i.title,
       EXISTS(SELECT 1 FROM review_appeals a WHERE a.review_id=r.id AND a.resolved_at IS NULL) AS pending,
       (r.hidden AND EXISTS (SELECT 1 FROM review_moderation_actions h
           WHERE h.id=(SELECT h2.id FROM review_moderation_actions h2
                       WHERE h2.review_id=r.id AND h2.action='hide' ORDER BY h2.created_at DESC,h2.id DESC LIMIT 1)
           AND NOT EXISTS (SELECT 1 FROM review_appeals a WHERE a.hide_action_id=h.id)))::boolean AS can_appeal,
       CASE WHEN r.author_id=sqlc.arg(actor)::uuid THEN coalesce((
           SELECT m.reason FROM review_moderation_actions m WHERE m.review_id=r.id AND m.action IN ('hide','uphold','restore')
           ORDER BY m.created_at DESC,m.id DESC LIMIT 1
       ),'') ELSE '' END::text AS decision_reason
FROM reviews r JOIN order_items i ON i.order_id=r.order_id
WHERE r.id=sqlc.arg(review_id) AND sqlc.arg(actor)::uuid IN (r.author_id,r.seller_id);

-- name: AddReviewReport :execrows
INSERT INTO review_reports (review_id,reporter_id,reason) VALUES ($1,$2,$3)
ON CONFLICT (review_id,reporter_id) DO NOTHING;

-- name: AddReviewAppeal :execrows
INSERT INTO review_appeals (review_id,hide_action_id,appellant_id,reason)
SELECT m.review_id,m.id,sqlc.arg(appellant_id)::uuid,sqlc.arg(reason)::text
FROM review_moderation_actions m
WHERE m.review_id=sqlc.arg(review_id)::uuid AND m.action='hide'
ORDER BY m.created_at DESC,m.id DESC LIMIT 1
ON CONFLICT DO NOTHING;

-- name: PendingReviewCases :many
SELECT r.id,r.body,i.title,r.hidden
FROM reviews r JOIN order_items i ON i.order_id=r.order_id
WHERE EXISTS (SELECT 1 FROM review_reports v WHERE v.review_id=r.id AND v.resolved_at IS NULL)
OR EXISTS (SELECT 1 FROM review_appeals a WHERE a.review_id=r.id AND a.resolved_at IS NULL)
ORDER BY r.created_at,r.id LIMIT 31 OFFSET sqlc.arg(page_offset)::integer;

-- name: ReviewCaseReasons :many
SELECT reason, 'Report'::text AS kind, created_at FROM review_reports rr WHERE rr.review_id=$1 AND rr.resolved_at IS NULL
UNION ALL
SELECT reason, 'Appeal'::text AS kind, created_at FROM review_appeals ra WHERE ra.review_id=$1 AND ra.resolved_at IS NULL
ORDER BY created_at LIMIT 100;

-- name: ResolveReviewReports :execrows
UPDATE review_reports SET resolved_at=now() WHERE review_id=$1 AND resolved_at IS NULL;

-- name: ResolveReviewAppeal :execrows
UPDATE review_appeals SET resolved_at=now() WHERE review_id=$1 AND resolved_at IS NULL;

-- name: SetReviewVisibility :exec
UPDATE reviews SET hidden=$2 WHERE id=$1;

-- name: AuditReviewAction :exec
INSERT INTO review_moderation_actions (review_id,moderator_id,action,reason) VALUES ($1,$2,$3,$4);

-- name: NotifyReview :exec
INSERT INTO review_notifications (user_id,review_id,kind) VALUES ($1,$2,$3);

-- name: ReviewNotices :many
SELECT n.review_id,n.kind,n.created_at,i.title FROM review_notifications n
JOIN reviews r ON r.id=n.review_id JOIN order_items i ON i.order_id=r.order_id
WHERE n.user_id=sqlc.arg(actor)::uuid AND sqlc.arg(actor)::uuid IN (r.author_id,r.seller_id)
ORDER BY n.created_at DESC,n.id LIMIT 30 OFFSET sqlc.arg(page_offset)::integer;

-- name: RecordListingView :exec
INSERT INTO listing_view_counts (listing_id,views)
SELECT id,1 FROM listings WHERE id=sqlc.arg(listing_id) AND status='published'
AND seller_id IS DISTINCT FROM sqlc.narg(viewer)::uuid
ON CONFLICT (listing_id) DO UPDATE SET views=listing_view_counts.views+1;

-- name: SellerActivity :one
SELECT (SELECT coalesce(sum(v.views),0)::bigint FROM listing_view_counts v
        JOIN listings l ON l.id=v.listing_id WHERE l.seller_id=sqlc.arg(seller_id))::bigint AS views,
       count(*) AS orders,
       count(*) FILTER (WHERE state='completed') AS completed
FROM orders WHERE seller_id=sqlc.arg(seller_id);

-- name: SellerGrossSales :many
SELECT i.currency,sum(i.price_minor)::bigint AS gross_minor
FROM orders o JOIN order_items i ON i.order_id=o.id
WHERE o.seller_id=$1 AND o.state='completed'
AND EXISTS (SELECT 1 FROM payment_observations p WHERE p.order_id=o.id)
GROUP BY i.currency ORDER BY i.currency;
