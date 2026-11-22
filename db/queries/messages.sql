-- name: OpenConversation :one
INSERT INTO conversations (listing_id, buyer_id, seller_id)
SELECT l.id, sqlc.arg(buyer_id), l.seller_id FROM listings l
WHERE l.id = sqlc.arg(listing_id) AND l.status = 'published' AND l.seller_id <> sqlc.arg(buyer_id)
ON CONFLICT (listing_id, buyer_id, seller_id) DO UPDATE SET listing_id = excluded.listing_id
RETURNING conversations.id;

-- name: Inbox :many
SELECT c.id, l.title, peer.username AS peer,
       EXISTS (SELECT 1 FROM messages m WHERE m.conversation_id = c.id AND m.sender_id <> sqlc.arg(actor)::uuid
           AND m.sequence > coalesce(r.last_sequence, 0)) AS unread
FROM conversations c JOIN listings l ON l.id = c.listing_id
JOIN users peer ON peer.id = CASE WHEN c.buyer_id = sqlc.arg(actor)::uuid THEN c.seller_id ELSE c.buyer_id END
LEFT JOIN conversation_reads r ON r.conversation_id = c.id AND r.user_id = sqlc.arg(actor)::uuid
WHERE sqlc.arg(actor)::uuid IN (c.buyer_id, c.seller_id)
ORDER BY c.updated_at DESC, c.id
LIMIT 30 OFFSET sqlc.arg(page_offset)::integer;

-- name: Thread :one
SELECT c.id, c.listing_id, l.title, peer.username AS peer,
       EXISTS (SELECT 1 FROM conversation_blocks b WHERE b.conversation_id = c.id) AS blocked,
       EXISTS (SELECT 1 FROM conversation_blocks b WHERE b.conversation_id = c.id AND b.user_id = sqlc.arg(actor)::uuid) AS own_block
FROM conversations c JOIN listings l ON l.id = c.listing_id
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
SELECT sqlc.arg(conversation_id)::uuid, sqlc.arg(sender_id)::uuid, sqlc.arg(body)::text, coalesce(max(m.sequence), 0) + 1
FROM messages m WHERE m.conversation_id = sqlc.arg(conversation_id)::uuid
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
SELECT n.id, n.conversation_id, n.message_id, l.title, n.created_at,
       m.sequence > coalesce(r.last_sequence, 0) AS unread
FROM notifications n JOIN messages m ON m.id = n.message_id
JOIN conversations c ON c.id = n.conversation_id JOIN listings l ON l.id = c.listing_id
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
