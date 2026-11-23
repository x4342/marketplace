-- name: EffectiveRole :one
SELECT marketplace_effective_role(sqlc.arg(account)::uuid)::text AS role;

-- name: LockRoleAdministration :exec
SELECT pg_advisory_xact_lock(5194898321094017);

-- name: BootstrappedAdmin :one
SELECT a.user_id, u.password_hash FROM administrator_bootstrap a JOIN users u ON a.user_id=u.id;

-- name: RecordBootstrap :exec
INSERT INTO administrator_bootstrap(user_id) VALUES($1);

-- name: SetRole :exec
INSERT INTO role_assignments(user_id,role) VALUES ($1,$2)
ON CONFLICT(user_id) DO UPDATE SET role=excluded.role;

-- name: RecordRoleChange :exec
INSERT INTO role_change_events(actor_id,subject_id,old_role,new_role,action) VALUES($1,$2,$3,$4,$5);

-- name: ActiveAdministratorExists :one
SELECT EXISTS (SELECT 1 FROM role_assignments WHERE role='admin')::boolean;
