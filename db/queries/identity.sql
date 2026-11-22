-- name: CreateUser :one
INSERT INTO users (username, password_hash)
VALUES ($1, $2)
RETURNING id;

-- name: UserCredential :one
SELECT id, password_hash FROM users WHERE username = $1;

-- name: LockUser :one
SELECT password_hash FROM users WHERE id = $1 FOR UPDATE;

-- name: UpdatePassword :exec
UPDATE users SET password_hash = $2 WHERE id = $1;

-- name: CreateSession :exec
INSERT INTO sessions (token_hash, user_id, expires_at)
VALUES ($1, $2, now() + interval '12 hours');

-- name: ActiveSession :one
SELECT u.id, u.username,
       EXISTS (SELECT 1 FROM role_assignments r WHERE r.user_id = u.id AND r.role = 'moderator') AS moderator,
       EXISTS (SELECT 1 FROM role_assignments r WHERE r.user_id = u.id AND r.role = 'finance_admin') AS finance_admin
FROM sessions s JOIN users u ON u.id = s.user_id
WHERE s.token_hash = $1 AND s.expires_at > now();

-- name: DeleteSession :execrows
DELETE FROM sessions WHERE token_hash = $1 AND user_id = $2;

-- name: DeleteAllSessions :exec
DELETE FROM sessions WHERE user_id = $1;

-- name: StoreRecoveryCode :exec
INSERT INTO recovery_codes (user_id, token_hash) VALUES ($1, $2);

-- name: ConsumeRecoveryCode :execrows
DELETE FROM recovery_codes WHERE user_id = $1 AND token_hash = $2;

-- name: DeleteRecoveryCodes :exec
DELETE FROM recovery_codes WHERE user_id = $1;

-- name: RecordSecurityEvent :exec
INSERT INTO security_events (user_id, action) VALUES ($1, $2);
