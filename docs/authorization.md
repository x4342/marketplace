# M12 authorization and local administrator setup

| Effective role | Permissions |
| --- | --- |
| `user` | Own profile, listings, orders, messages, reviews, favorites, and earnings. Every registration starts here. |
| `moderator` | User permissions plus content cases, dispute and appeal queues, payout review and decisions, simulated adjustments and reconciliation, and staff audit. |
| `admin` | Moderator permissions plus the platform capability. M12 uses it for protected system health and local role operations. Platform dashboard and category management UI are outside M12. |

HTTP guards and service transactions use the effective role, with separate ownership and conflict-of-interest checks. Staff enter their current password on every financial mutation, including hold, release, refund, payout and reconciliation. Password confirmations are rate-limited. An administrator or moderator cannot act on their own payout or adjust a transaction they participated in. Appeal reviews require a different staff member from the original decision maker. Platform-only routes check the admin capability server-side.

## Inspect and approve legacy staff

Before applying migration 00012, use the local PostgreSQL console to inspect the old assignments in a private terminal.

```sh
docker compose -p marketplace -f compose.portainer.yaml exec postgres psql -U marketplace -d marketplace
```

```sql
SELECT u.username, array_agg(r.role ORDER BY r.role) AS previous_roles
FROM role_assignments r JOIN users u ON u.id=r.user_id
GROUP BY u.username ORDER BY u.username;
```

Migration 00012 preserves exact legacy rows in `legacy_role_assignments`. Accounts with both previous grants retain their equivalent combined permissions as moderators. A single legacy grant provides no new staff access. The account operates as a user until the owner approves the combined moderator scope for that named account and runs the explicit grant. No legacy assignment creates an administrator. Existing sessions resolve the effective role at request time, and explicit grants revoke the target's sessions.

The archived grants and role-change events are protected from normal update or deletion. Historical audit events keep their original actor IDs. Migration 00012 supports a clean, empty rollback for CI and refuses rollback when it would discard meaningful role or authentication history. Read the upgrade and recovery runbook before replacing an image on an existing database.

## Bootstrap after the migration

Run on the Docker host or in Portainer's interactive container console with the matching M12 image. The normal app startup never runs this tool. The tool requires a terminal with hidden secret entry. Store the one-time recovery codes shown by successful bootstrap in a password manager.

```sh
docker compose -p marketplace -f compose.portainer.yaml exec web /app/marketplace-admin bootstrap
```

For a preexisting `admin` username, the regular command stops without changing the account. After verifying ownership, use the explicit claim command. It requires the existing password and an unused recovery code, revokes old sessions, replaces recovery codes, and keeps the existing password.

```sh
docker compose -p marketplace -f compose.portainer.yaml exec web /app/marketplace-admin bootstrap --claim-existing
```

If Portainer manages the stack without a local Compose checkout, use its web container's **Console**, enable interactive/TTY, and choose `/app/marketplace-admin` with argument `bootstrap`. You can also resolve the container by its labels and use Docker directly.

```sh
web=$(docker ps -q --filter label=com.docker.compose.project=marketplace --filter label=com.docker.compose.service=web)
test -n "$web" && docker exec -it "$web" /app/marketplace-admin bootstrap
```

After approving a named account for the combined role, use the exact account name with the local CLI. Substitute the same `docker exec -it "$web"` prefix on a Portainer-managed host.

```sh
docker compose -p marketplace -f compose.portainer.yaml exec web /app/marketplace-admin set-role USERNAME moderator
docker compose -p marketplace -f compose.portainer.yaml exec web /app/marketplace-admin set-role USERNAME user
```

For password recovery, use `/recover` with a saved one-time code. Recovery rotates sessions and returns replacement codes. If the original administrator's role was removed while the bootstrap marker remains, use `restore-admin` with the original administrator's current password. The tool can only restore the original account. If password and recovery codes are both unavailable, use a trusted database backup or an independently authorized recovery procedure.

```sh
docker compose -p marketplace -f compose.portainer.yaml exec web /app/marketplace-admin restore-admin
```

Reference [OWASP reauthentication guidance](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html#reauthentication-after-risk-events).
