-- name: PublicProfile :one
SELECT u.id, u.username, u.created_at, coalesce(p.bio,'')::text AS bio
FROM users u LEFT JOIN user_profiles p ON p.user_id=u.id
WHERE u.username=$1 AND NOT EXISTS (SELECT 1 FROM user_restrictions r WHERE r.user_id=u.id);

-- name: PublicAvatar :one
SELECT p.avatar_jpeg FROM user_profiles p JOIN users u ON u.id=p.user_id
WHERE u.username=$1 AND p.avatar_jpeg IS NOT NULL
AND NOT EXISTS (SELECT 1 FROM user_restrictions r WHERE r.user_id=u.id);

-- name: ProfileListings :many
SELECT id, title, price_minor, currency FROM listings
WHERE seller_id=$1 AND status='published'
ORDER BY published_at DESC, id LIMIT 25 OFFSET sqlc.arg(page_offset)::integer;

-- name: OwnProfile :one
SELECT coalesce(p.bio,'')::text AS bio, (p.avatar_jpeg IS NOT NULL)::boolean AS has_avatar
FROM users u LEFT JOIN user_profiles p ON p.user_id=u.id WHERE u.id=$1;

-- name: SaveProfile :exec
INSERT INTO user_profiles (user_id,bio,avatar_jpeg)
VALUES (sqlc.arg(user_id),sqlc.arg(bio),sqlc.narg(avatar_jpeg)::bytea)
ON CONFLICT (user_id) DO UPDATE SET bio=excluded.bio,
  avatar_jpeg=CASE WHEN sqlc.arg(remove_avatar)::boolean THEN NULL
              ELSE coalesce(excluded.avatar_jpeg,user_profiles.avatar_jpeg) END;

-- name: OwnAvatar :one
SELECT avatar_jpeg FROM user_profiles WHERE user_id=$1 AND avatar_jpeg IS NOT NULL;
