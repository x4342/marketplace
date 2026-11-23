-- name: OpenConversation :one
INSERT INTO conversations (listing_id, buyer_id, seller_id)
SELECT l.id, sqlc.arg(buyer_id), l.seller_id FROM listings l
WHERE l.id = sqlc.arg(listing_id) AND l.status = 'published' AND l.seller_id <> sqlc.arg(buyer_id)
AND NOT EXISTS (SELECT 1 FROM user_restrictions r WHERE r.user_id IN (l.seller_id,sqlc.arg(buyer_id)::uuid))
ON CONFLICT (listing_id, buyer_id, seller_id) DO UPDATE SET listing_id = excluded.listing_id
RETURNING conversations.id;

-- name: Inbox :many
SELECT c.id, coalesce(l.title,'')::text AS title, peer.username AS peer, c.updated_at,
       coalesce((SELECT left(m.body,110) FROM messages m WHERE m.conversation_id=c.id ORDER BY m.sequence DESC LIMIT 1),'')::text AS preview,
       EXISTS (SELECT 1 FROM messages m WHERE m.conversation_id = c.id AND m.sender_id <> sqlc.arg(actor)::uuid
           AND m.sequence > coalesce(r.last_sequence, 0)) AS unread
FROM conversations c LEFT JOIN listings l ON l.id = c.listing_id
JOIN users peer ON peer.id = CASE WHEN c.buyer_id = sqlc.arg(actor)::uuid THEN c.seller_id ELSE c.buyer_id END
LEFT JOIN conversation_reads r ON r.conversation_id = c.id AND r.user_id = sqlc.arg(actor)::uuid
WHERE sqlc.arg(actor)::uuid IN (c.buyer_id, c.seller_id)
ORDER BY c.updated_at DESC, c.id
LIMIT 31 OFFSET sqlc.arg(page_offset)::integer;

-- name: Thread :one
SELECT c.id, c.listing_id, coalesce(l.title,'')::text AS title, peer.username AS peer,
       (c.moderation_blocked OR EXISTS (SELECT 1 FROM conversation_blocks b WHERE b.conversation_id = c.id)
        OR EXISTS (SELECT 1 FROM user_restrictions r WHERE r.user_id IN (c.buyer_id,c.seller_id)))::boolean AS blocked,
       EXISTS (SELECT 1 FROM conversation_blocks b WHERE b.conversation_id = c.id AND b.user_id = sqlc.arg(actor)::uuid) AS own_block
FROM conversations c LEFT JOIN listings l ON l.id = c.listing_id
JOIN users peer ON peer.id = CASE WHEN c.buyer_id = sqlc.arg(actor)::uuid THEN c.seller_id ELSE c.buyer_id END
WHERE c.id = sqlc.arg(thread_id) AND sqlc.arg(actor)::uuid IN (c.buyer_id, c.seller_id);

-- name: ThreadMessages :many
SELECT m.id, m.sequence, m.body, m.sent_at, u.username AS sender
FROM messages m JOIN users u ON u.id = m.sender_id
JOIN conversations c ON c.id = m.conversation_id
WHERE c.id = sqlc.arg(thread_id) AND sqlc.arg(actor)::uuid IN (c.buyer_id, c.seller_id)
  AND m.sequence < sqlc.arg(before_sequence)::bigint
ORDER BY m.sequence DESC LIMIT 31;

-- name: LockConversation :one
SELECT c.seller_id, c.buyer_id
FROM conversations c
WHERE c.id = sqlc.arg(thread_id) AND sqlc.arg(actor)::uuid IN (c.buyer_id, c.seller_id)
FOR UPDATE;

-- name: AddMessage :one
INSERT INTO messages (conversation_id, sender_id, body, sequence)
SELECT c.id, sqlc.arg(sender_id)::uuid, sqlc.arg(body)::text,
       coalesce((SELECT max(m.sequence) FROM messages m WHERE m.conversation_id=c.id),0) + 1
FROM conversations c WHERE c.id=sqlc.arg(conversation_id)::uuid
AND sqlc.arg(sender_id)::uuid IN (c.buyer_id,c.seller_id)
RETURNING id;

-- name: AddMessageNotification :exec
INSERT INTO notifications (user_id, conversation_id, message_id) VALUES ($1, $2, $3);

-- name: TouchConversation :exec
UPDATE conversations SET updated_at = now() WHERE id = $1;

-- name: BlockConversation :exec
INSERT INTO conversation_blocks (conversation_id, user_id)
VALUES ($1, $2) ON CONFLICT DO NOTHING;

-- name: UnblockConversation :exec
DELETE FROM conversation_blocks WHERE conversation_id = $1 AND user_id = $2;

-- name: AcknowledgeConversation :exec
INSERT INTO conversation_reads (conversation_id, user_id, last_sequence)
SELECT c.id, sqlc.arg(actor)::uuid, coalesce(max(m.sequence), 0)::bigint
FROM conversations c LEFT JOIN messages m ON m.conversation_id = c.id AND m.sequence <= sqlc.arg(last_sequence)::bigint
WHERE c.id = sqlc.arg(thread_id) AND sqlc.arg(actor)::uuid IN (c.buyer_id, c.seller_id)
GROUP BY c.id
ON CONFLICT (conversation_id, user_id) DO UPDATE
SET last_sequence = greatest(conversation_reads.last_sequence, excluded.last_sequence);

-- name: ReportConversation :exec
INSERT INTO reports (reporter_id, conversation_id, reason)
SELECT sqlc.arg(actor)::uuid, c.id, sqlc.arg(reason)::text
FROM conversations c WHERE c.id = sqlc.arg(thread_id) AND sqlc.arg(actor)::uuid IN (c.buyer_id, c.seller_id);

-- name: MessageNotifications :many
SELECT n.id, n.conversation_id, n.message_id, coalesce(l.title,'')::text AS title, u.username AS sender, n.created_at,
       m.sequence > coalesce(r.last_sequence, 0) AS unread
FROM notifications n JOIN messages m ON m.id = n.message_id
JOIN conversations c ON c.id = n.conversation_id LEFT JOIN listings l ON l.id = c.listing_id
JOIN users u ON u.id=m.sender_id
LEFT JOIN conversation_reads r ON r.conversation_id = c.id AND r.user_id = sqlc.arg(actor)::uuid
WHERE n.user_id = sqlc.arg(actor)::uuid AND sqlc.arg(actor)::uuid IN (c.buyer_id, c.seller_id)
ORDER BY n.created_at DESC, n.id LIMIT 30 OFFSET sqlc.arg(page_offset)::integer;

-- name: MessageNotificationDestination :one
SELECT n.conversation_id, n.message_id, m.sequence
FROM notifications n JOIN messages m ON m.id = n.message_id
JOIN conversations c ON c.id = n.conversation_id
WHERE n.id = sqlc.arg(notification_id) AND n.user_id = sqlc.arg(actor)::uuid
AND sqlc.arg(actor)::uuid IN (c.buyer_id, c.seller_id);

-- name: ThreadEvents :many
SELECT e.title_snapshot, e.created_at
FROM conversation_system_events e JOIN conversations c ON c.id = e.conversation_id
WHERE c.id = sqlc.arg(thread_id) AND sqlc.arg(actor)::uuid IN (c.buyer_id, c.seller_id)
ORDER BY e.created_at DESC, e.id DESC LIMIT 30;

-- name: UnreadConversations :one
SELECT count(*) FROM conversations c
LEFT JOIN conversation_reads r ON r.conversation_id = c.id AND r.user_id = sqlc.arg(actor)::uuid
WHERE sqlc.arg(actor)::uuid IN (c.buyer_id, c.seller_id) AND EXISTS (
    SELECT 1 FROM messages m WHERE m.conversation_id = c.id AND m.sender_id <> sqlc.arg(actor)::uuid
        AND m.sequence > coalesce(r.last_sequence, 0)
);

-- name: LockMessagingActor :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(actor)::text, 14016));

-- name: DirectPeer :one
SELECT u.id FROM users u WHERE u.username=sqlc.arg(username)
AND u.id<>sqlc.arg(actor)::uuid
AND NOT EXISTS (SELECT 1 FROM user_restrictions r WHERE r.user_id IN (u.id,sqlc.arg(actor)::uuid));

-- name: DirectConversation :one
SELECT id FROM conversations
WHERE listing_id IS NULL AND buyer_id=least(sqlc.arg(actor)::uuid,sqlc.arg(peer)::uuid)
AND seller_id=greatest(sqlc.arg(actor)::uuid,sqlc.arg(peer)::uuid);

-- name: CreateDirectConversation :one
INSERT INTO conversations (buyer_id,seller_id,direct_opened_by,direct_opened_at)
SELECT least(sqlc.arg(actor)::uuid,sqlc.arg(peer)::uuid),
       greatest(sqlc.arg(actor)::uuid,sqlc.arg(peer)::uuid),sqlc.arg(actor)::uuid,now()
WHERE sqlc.arg(actor)::uuid <> sqlc.arg(peer)::uuid
AND NOT EXISTS (SELECT 1 FROM user_restrictions r WHERE r.user_id IN (sqlc.arg(actor)::uuid,sqlc.arg(peer)::uuid))
ON CONFLICT (buyer_id,seller_id) WHERE listing_id IS NULL
DO UPDATE SET listing_id=NULL RETURNING id;

-- name: DirectOpenLimited :one
SELECT EXISTS (SELECT 1 FROM conversations WHERE direct_opened_by=sqlc.arg(actor)::uuid
AND listing_id IS NULL AND direct_opened_at>=now()-interval '1 minute'
ORDER BY direct_opened_at DESC OFFSET 9 LIMIT 1)::boolean;

-- name: SendMessageLimited :one
SELECT EXISTS (SELECT 1 FROM messages WHERE sender_id=sqlc.arg(actor)::uuid
AND sent_at>=now()-interval '1 minute' ORDER BY sent_at DESC OFFSET 29 LIMIT 1)::boolean;

-- name: LatestConversation :one
SELECT id FROM (
  (SELECT id,updated_at FROM conversations WHERE buyer_id=sqlc.arg(actor)::uuid ORDER BY updated_at DESC,id LIMIT 1)
  UNION ALL
  (SELECT id,updated_at FROM conversations WHERE seller_id=sqlc.arg(actor)::uuid ORDER BY updated_at DESC,id LIMIT 1)
) participants ORDER BY updated_at DESC,id LIMIT 1;
