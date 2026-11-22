-- name: WorkerHeartbeat :one
SELECT observed_at FROM worker_heartbeat WHERE singleton;

-- name: RecordWorkerHeartbeat :exec
INSERT INTO worker_heartbeat (singleton, observed_at)
VALUES (true, clock_timestamp())
ON CONFLICT (singleton) DO UPDATE
SET observed_at = clock_timestamp();
