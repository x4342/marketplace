-- name: PostAdminAudit :exec
INSERT INTO admin_audit_events(actor_id,area,subject_id,action,reason) VALUES($1,$2,$3,$4,$5);

-- name: AdminAudit :many
SELECT a.area,a.subject_id,a.action,a.reason,a.created_at,u.username
FROM admin_audit_events a JOIN users u ON u.id=a.actor_id
WHERE (sqlc.arg(content)::boolean AND a.area IN ('reviews','dispute','content'))
   OR (sqlc.arg(finance)::boolean AND a.area IN ('payouts','payment'))
ORDER BY a.id DESC LIMIT 31 OFFSET sqlc.arg(page_offset)::integer;

-- name: OpenOrderDispute :one
INSERT INTO disputes(order_id,category) VALUES($1,$2)
ON CONFLICT(order_id) DO NOTHING RETURNING id;

-- name: OrderDispute :one
SELECT id FROM disputes WHERE order_id=$1;

-- name: LockDispute :one
SELECT * FROM disputes WHERE id=$1 FOR UPDATE;

-- name: DisputeRecord :one
SELECT d.id,d.order_id,d.status,d.category,d.decided_by,i.title,i.currency,i.price_minor,i.kind,i.delivery_mode,i.terms,o.buyer_id,o.seller_id,
       EXISTS(SELECT 1 FROM shipments s WHERE s.order_id=o.id) AS dispatch_recorded,
       EXISTS(SELECT 1 FROM manual_deliveries m WHERE m.order_id=o.id AND m.payload IS NOT NULL) AS manual_delivery_recorded,
       EXISTS(SELECT 1 FROM digital_entitlements e WHERE e.order_id=o.id) AS download_granted
FROM disputes d JOIN orders o ON o.id=d.order_id JOIN order_items i ON i.order_id=o.id
WHERE d.id=$1;

-- name: DisputesQueue :many
SELECT d.id,d.status,d.category,i.title, d.updated_at,
       EXISTS(SELECT 1 FROM dispute_appeals a WHERE a.dispute_id=d.id AND a.outcome='pending') AS pending_appeal
FROM disputes d JOIN order_items i ON i.order_id=d.order_id
WHERE sqlc.arg(state)::text='' OR d.status=sqlc.arg(state)::text
ORDER BY d.updated_at DESC,d.id LIMIT 31 OFFSET sqlc.arg(page_offset)::integer;

-- name: SetDisputeOutcome :exec
UPDATE disputes SET status=$2,decided_by=$3,updated_at=now() WHERE id=$1;

-- name: PostDisputeEvent :exec
INSERT INTO dispute_events(dispute_id,actor_id,action,detail) VALUES($1,$2,$3,$4);

-- name: DisputeHistory :many
SELECT e.action,e.detail,e.created_at,coalesce(u.username,'System')::text AS actor
FROM dispute_events e LEFT JOIN users u ON u.id=e.actor_id WHERE e.dispute_id=$1 ORDER BY e.id;

-- name: AddCaseEvidence :exec
INSERT INTO case_evidence(dispute_id,actor_id,kind,body) VALUES($1,$2,$3,$4);

-- name: CaseEvidence :many
SELECT e.kind,e.body,e.created_at,u.username FROM case_evidence e JOIN users u ON u.id=e.actor_id
WHERE e.dispute_id=$1 ORDER BY e.created_at,e.id LIMIT 100;

-- name: AddDisputeAppeal :execrows
INSERT INTO dispute_appeals(dispute_id,actor_id,reason) VALUES($1,$2,$3)
ON CONFLICT(dispute_id,actor_id) DO NOTHING;

-- name: DisputeAppeals :many
SELECT id,actor_id,reason,outcome,created_at FROM dispute_appeals WHERE dispute_id=$1 ORDER BY created_at,id;

-- name: ResolveDisputeAppeal :execrows
UPDATE dispute_appeals SET outcome=sqlc.arg(outcome)
WHERE id=sqlc.arg(id) AND dispute_id=sqlc.arg(dispute_id) AND outcome='pending';

-- name: NotifyDispute :exec
INSERT INTO dispute_notices(user_id,dispute_id,label)
SELECT o.buyer_id,d.id,sqlc.arg(label)::text FROM disputes d JOIN orders o ON o.id=d.order_id WHERE d.id=sqlc.arg(dispute_id)
UNION ALL
SELECT o.seller_id,d.id,sqlc.arg(label)::text FROM disputes d JOIN orders o ON o.id=d.order_id WHERE d.id=sqlc.arg(dispute_id);

-- name: UserDisputeNotices :many
SELECT n.dispute_id,n.label,n.created_at FROM dispute_notices n JOIN disputes d ON d.id=n.dispute_id
JOIN orders o ON o.id=d.order_id
WHERE n.user_id=sqlc.arg(actor)::uuid AND sqlc.arg(actor)::uuid IN (o.buyer_id,o.seller_id)
ORDER BY n.id DESC LIMIT 30 OFFSET sqlc.arg(page_offset)::integer;

-- name: SetOrderUnderReview :exec
UPDATE orders SET state='under_review' WHERE id=$1 AND state IN ('disputed','fulfillment_failed');

-- name: PublicContentTarget :one
SELECT id,seller_id FROM listings WHERE id=$1 AND status='published';

-- name: ReportListingOrSeller :execrows
INSERT INTO content_reports(reporter_id,listing_id,subject_user_id,reason)
VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING;

-- name: ContentQueue :many
SELECT r.id,r.reason,r.state,r.created_at,coalesce(l.title,u.username,'Conversation')::text AS subject,
       EXISTS(SELECT 1 FROM content_appeals a WHERE a.report_id=r.id AND a.resolved_at IS NULL) AS pending_appeal
FROM content_reports r LEFT JOIN listings l ON l.id=r.listing_id LEFT JOIN users u ON u.id=r.subject_user_id
WHERE sqlc.arg(state)::text='' OR r.state=sqlc.arg(state)::text
ORDER BY r.created_at DESC,r.id LIMIT 31 OFFSET sqlc.arg(page_offset)::integer;

-- name: ContentReportRecord :one
SELECT r.id,r.reason,r.state,r.reporter_id,r.decided_by,r.listing_id,r.subject_user_id,r.conversation_id,
       coalesce(l.title,u.username,'Conversation')::text AS subject,
       coalesce(l.seller_id,r.subject_user_id,c.seller_id) AS affected,
       c.buyer_id AS conversation_buyer
FROM content_reports r LEFT JOIN listings l ON l.id=r.listing_id LEFT JOIN users u ON u.id=r.subject_user_id
LEFT JOIN conversations c ON c.id=r.conversation_id WHERE r.id=$1 FOR UPDATE OF r;

-- name: UserContentCases :many
SELECT r.id,r.state,r.reason,coalesce(l.title,u.username,'Conversation')::text AS subject,
       EXISTS(SELECT 1 FROM content_appeals a WHERE a.report_id=r.id AND a.actor_id=sqlc.arg(actor)::uuid) AS appealed
FROM content_reports r LEFT JOIN listings l ON l.id=r.listing_id LEFT JOIN users u ON u.id=r.subject_user_id
LEFT JOIN conversations c ON c.id=r.conversation_id
WHERE sqlc.arg(actor)::uuid IN (l.seller_id,r.subject_user_id,c.seller_id,c.buyer_id)
AND r.state IN ('restricted','upheld','restored')
ORDER BY r.created_at DESC,r.id LIMIT 31 OFFSET sqlc.arg(page_offset)::integer;

-- name: SetListingModeration :exec
UPDATE listings SET moderation_locked=$2,
    status=CASE WHEN $2 AND status='published' THEN 'paused' ELSE status END,
    updated_at=now() WHERE id=$1;

-- name: SetConversationModeration :exec
UPDATE conversations SET moderation_blocked=$2 WHERE id=$1;

-- name: RestrictUser :exec
INSERT INTO user_restrictions(user_id,moderator_id,reason) VALUES($1,$2,$3)
ON CONFLICT(user_id) DO UPDATE SET moderator_id=excluded.moderator_id,reason=excluded.reason;

-- name: UnrestrictUser :exec
DELETE FROM user_restrictions WHERE user_id=$1;

-- name: UserRestricted :one
SELECT EXISTS(SELECT 1 FROM user_restrictions WHERE user_id=$1)::boolean;

-- name: SetContentOutcome :exec
UPDATE content_reports SET state=$2,decided_by=$3 WHERE id=$1;

-- name: AddContentAppeal :execrows
INSERT INTO content_appeals(report_id,actor_id,reason) VALUES($1,$2,$3) ON CONFLICT DO NOTHING;

-- name: PendingContentAppeals :many
SELECT reason,created_at FROM content_appeals WHERE report_id=$1 AND resolved_at IS NULL ORDER BY created_at,id;

-- name: ResolveContentAppeals :execrows
UPDATE content_appeals SET resolved_at=now() WHERE report_id=$1 AND resolved_at IS NULL;

-- name: ContentActionReason :one
SELECT reason FROM admin_audit_events WHERE subject_id=$1 AND area='content' AND action IN ('restrict','uphold','restore')
ORDER BY id DESC LIMIT 1;

-- name: OtherContentRestriction :one
SELECT EXISTS (
    SELECT 1 FROM content_reports old JOIN content_reports current ON current.id=sqlc.arg(report_id)
    WHERE old.id<>current.id AND old.state IN ('restricted','upheld')
      AND (old.listing_id=current.listing_id OR old.subject_user_id=current.subject_user_id OR old.conversation_id=current.conversation_id)
)::boolean;

-- name: LockContentTarget :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(target)::uuid::text, 88008));

-- name: PublicReportableListing :one
SELECT id,seller_id FROM listings WHERE id=$1 AND status='published';
