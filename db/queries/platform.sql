-- name: LockCategories :exec
SELECT pg_advisory_xact_lock(5194898321094018);

-- name: LockCategoriesForPublication :exec
SELECT pg_advisory_xact_lock_shared(5194898321094018);

-- name: CanonicalCategory :one
SELECT coalesce(
    (SELECT slug FROM categories WHERE categories.slug=sqlc.arg(input)),
    (SELECT c.slug FROM category_slug_history h JOIN categories c ON c.id=h.category_id WHERE h.slug=sqlc.arg(input)),
    sqlc.arg(input))::text AS slug;

-- name: OwnedListingCategory :one
SELECT coalesce((SELECT category_slug FROM listings
    WHERE id=sqlc.arg(listing_id) AND seller_id=sqlc.arg(owner_id) AND status IN ('published','paused')), '')::text;

-- name: AdminCategories :many
SELECT c.id,c.slug,c.name,c.description,c.display_order,c.active,c.revision,
       c.parent_slug, p.name AS parent_name,
       (SELECT count(*) FROM listings l WHERE l.category_slug=c.slug)::bigint AS listing_count
FROM categories c LEFT JOIN categories p ON p.slug=c.parent_slug
WHERE marketplace_effective_role(sqlc.arg(actor)::uuid)='admin'
ORDER BY coalesce(p.display_order,c.display_order), coalesce(p.name,c.name),
         coalesce(p.slug,c.slug), c.parent_slug NULLS FIRST, c.display_order, c.name, c.slug;

-- name: AdminCategory :one
SELECT c.* FROM categories c
WHERE c.id=sqlc.arg(id) AND marketplace_effective_role(sqlc.arg(actor)::uuid)='admin';

-- name: CreateCategory :one
INSERT INTO categories (slug,name,description,parent_slug,display_order,active)
SELECT sqlc.arg(slug),sqlc.arg(name),sqlc.arg(description),sqlc.narg(parent_slug)::text,sqlc.arg(display_order),sqlc.arg(active)
WHERE marketplace_effective_role(sqlc.arg(actor)::uuid)='admin'
RETURNING id;

-- name: UpdateCategory :execrows
UPDATE categories SET slug=sqlc.arg(slug),name=sqlc.arg(name),description=sqlc.arg(description),
    parent_slug=sqlc.narg(parent_slug),display_order=sqlc.arg(display_order),active=sqlc.arg(active),revision=revision+1
WHERE id=sqlc.arg(id) AND revision=sqlc.arg(revision) AND marketplace_effective_role(sqlc.arg(actor)::uuid)='admin';

-- name: RecordCategoryChange :exec
INSERT INTO category_change_events (actor_id,category_id,before_state,after_state)
SELECT sqlc.arg(actor), c.id, sqlc.narg(before_state)::jsonb, to_jsonb(c)
FROM categories c
WHERE c.id=sqlc.arg(id) AND marketplace_effective_role(sqlc.arg(actor)::uuid)='admin';

-- name: CategoryAuditState :one
SELECT to_jsonb(c)::jsonb FROM categories c
WHERE c.id=sqlc.arg(id) AND marketplace_effective_role(sqlc.arg(actor)::uuid)='admin';

-- name: AdminUsers :many
SELECT u.username, u.created_at, marketplace_effective_role(u.id)::text AS role
FROM users u
WHERE marketplace_effective_role(sqlc.arg(actor)::uuid)='admin'
  AND starts_with(u.username,sqlc.arg(prefix)::text)
ORDER BY u.username LIMIT 31 OFFSET sqlc.arg(page_offset)::integer;

-- name: AdminRoleAudit :many
SELECT a.username AS actor, s.username AS subject, e.old_role, e.new_role, e.action, e.created_at
FROM role_change_events e JOIN users a ON a.id=e.actor_id JOIN users s ON s.id=e.subject_id
WHERE marketplace_effective_role(sqlc.arg(actor)::uuid)='admin'
ORDER BY e.id DESC LIMIT 10;

-- name: AdminCategoryAudit :many
SELECT u.username AS actor, coalesce(e.before_state->>'slug','')::text AS old_slug,
       (e.after_state->>'slug')::text AS slug, e.created_at
FROM category_change_events e JOIN users u ON u.id=e.actor_id
WHERE marketplace_effective_role(sqlc.arg(actor)::uuid)='admin'
ORDER BY e.id DESC LIMIT 10;

-- name: AdministratorCount :one
SELECT count(*)::bigint FROM role_assignments WHERE role='admin';
