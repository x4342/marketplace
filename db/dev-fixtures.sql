-- Fixed stale heartbeat for local verification of the worker's first update.
INSERT INTO worker_heartbeat (singleton, observed_at)
VALUES (true, '2000-01-01T00:00:00Z')
ON CONFLICT (singleton) DO NOTHING;
