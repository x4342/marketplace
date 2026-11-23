# M13 administrator dashboard and local upgrade

M11 established the single dark theme and square listing media pipeline. M12 added public profiles, avatars, three effective roles and local administrator bootstrap. M13 makes Discover the default page. Staff use `/staff` for moderation, disputes and simulated finance. Administrator-only controls and operational status live at `/admin`. The legacy status fragment requires an administrator session. Docker still probes `/health/live`, `/health/ready` and the worker heartbeat.

## Category and role behavior

**Account → Administrator dashboard** provides system health, recent role and category changes, deployment information, and links to staff workspaces. Platform configuration reflects the running implementation. Manage deployment settings through the existing local stack.

**Categories** supports top-level categories and one level of subcategories. Leave the slug blank to generate one from the name, or supply lowercase ASCII letters, digits and single hyphens. The display order starts at zero. A category or parent marked inactive stops appearing as a new seller choice. Published listings remain available. Sellers can keep an existing listing's inactive category when editing it. The admin list shows the direct count of listings across all states for each category. Category edits show a confirmation dialog, with an explicit confirmation checkbox available without JavaScript.

Migration `00013` assigns stable category UUIDs, adds metadata and revision checks, and reserves previous slugs to their original category. Renaming updates listing and child references in the same transaction. Old filter links and draft slugs resolve to the current slug. Listing IDs, old order snapshots, attachments, media and financial data are preserved. The app exposes retirement by inactivation and has no category deletion action. Changes record the actor and before/after values in append-only history.

**Users and roles** requires the administrator's current password and confirmation. The transaction rejects stale roles and removal of the last administrator, revokes changed accounts' sessions, and records assignments and removals in `role_change_events`. The original reserved username and local provisioning rules are described in the [bootstrap and recovery guide](authorization.md).

## 1. Back up the existing installation

Use the Arch Linux host's terminal. In Portainer, stop the `web` and `worker` containers to quiesce writes and leave PostgreSQL running. If the stack name differs, substitute it in each Docker label filter. Record the current image reference, Git revision, and PostgreSQL volume name from Portainer.

```sh
umask 077
pg=$(sudo docker ps -q --filter label=com.docker.compose.project=marketplace \
  --filter label=com.docker.compose.service=postgres)
test -n "$pg" || { echo "Marketplace PostgreSQL container is unavailable"; exit 1; }
backup="$HOME/marketplace-before-m13-$(date +%Y%m%d-%H%M%S).dump"
sudo docker exec "$pg" pg_dump -Fc -U marketplace -d marketplace > "$backup" &&
  test -s "$backup" &&
  sudo docker exec -i "$pg" pg_restore --list < "$backup" > /dev/null
```

Continue after all three backup checks succeed. Keep the archive private. If upgrading directly from a pre-M12 schema, review legacy staff grants before deploying the new image.

## 2. Build the approved image

Run from an authenticated checkout on the same Docker host. Set `approved_revision` to the full SHA the owner approved for deployment. Use the printed image tag in Portainer.

```sh
git fetch origin
approved_revision=PASTE_APPROVED_COMMIT_SHA
git worktree add --detach ../marketplace-approved "$approved_revision"
revision=$(git -C ../marketplace-approved rev-parse HEAD)
image="marketplace:sha-$revision"
sudo docker build --tag "$image" ../marketplace-approved
printf '%s\n' "$image"
```

A private, versioned registry image is also supported. See the [registry instructions](local-container-deployment.md#optional-private-ghcr-image).

## 3. Redeploy the existing Portainer stack

Open **Stacks → marketplace**. Retain the current stack name, `MARKETPLACE_DB_PASSWORD` and current PostgreSQL volume. If `MARKETPLACE_DB_VOLUME` was unset, the default is `marketplace_marketplace-data`. Update `MARKETPLACE_IMAGE` to the tag from step 2. For a Git stack, select the approved Git reference and use **Pull and redeploy**. Use the already-built local image when deploying a local tag. For a Web Editor or uploaded stack, apply the approved `compose.portainer.yaml` and update the existing stack. [Portainer stack editing](https://docs.portainer.io/user/docker/stacks/edit)

The migrator runs before the new web and worker services. Treat an unsuccessful migrator exit as a failed upgrade and inspect its logs in Portainer. Preserve the database and its volume during troubleshooting.

## 4. Initialize or recover the administrator

If the original admin is already provisioned, sign in with that account. For first-time setup, run this with the migrated M13 image:

```sh
web=$(sudo docker ps -q --filter label=com.docker.compose.project=marketplace \
  --filter label=com.docker.compose.service=web)
test -n "$web" && sudo docker exec -it "$web" /app/marketplace-admin bootstrap
```

Enter a password at the hidden prompt and store the displayed recovery codes privately. For a preexisting account with username `admin`, use `bootstrap --claim-existing` and supply the existing password and an unused recovery code. Use `/recover` and a saved recovery code for password recovery. Use the local `restore-admin` command only when the bootstrap marker remains and no active administrator exists. See the [full recovery instructions](authorization.md).

## 5. Verify the new installation

```sh
curl -fsS http://127.0.0.1:8080/health/live
curl -fsS http://127.0.0.1:8080/health/ready
curl -s -o /dev/null -w '%{http_code} %{redirect_url}\n' http://127.0.0.1:8080/
worker=$(sudo docker ps -q --filter label=com.docker.compose.project=marketplace \
  --filter label=com.docker.compose.service=worker)
test -n "$worker" && sudo docker exec "$worker" /app/container-health worker
```

Expect `ok`, `ready`, a redirect to Discover, and a successful worker check. In Portainer, check that the migrator exited zero and PostgreSQL and the worker have no published host ports. Sign in, open `/admin`, and check the current heartbeat. Visit a listing and a profile with a known image. Sign out and confirm that Discover is available and `/admin` redirects to sign-in.

## Rollback with the verified archive

Stop the upgraded web and worker. Keep the upgraded database volume available. Use the previous approved Compose file and create a private `recovery.env` file with mode 0600 containing the previous `MARKETPLACE_IMAGE`, the same `MARKETPLACE_DB_PASSWORD` and a new unused `MARKETPLACE_DB_VOLUME` name. Restore into that fresh volume:

```sh
sudo docker compose --env-file recovery.env -p marketplace-recovery -f compose.portainer.yaml up -d --wait postgres
sudo docker compose --env-file recovery.env -p marketplace-recovery -f compose.portainer.yaml exec -T postgres \
  pg_restore --single-transaction --exit-on-error -U marketplace -d marketplace < "$backup"
sudo docker compose --env-file recovery.env -p marketplace-recovery -f compose.portainer.yaml down
```

Point the original Portainer stack at the recovery volume and previous image and Git revision. Redeploy and repeat the health and listing checks. The restored database contains the state at backup time. A populated M13 installation uses archive restoration to avoid losing category and role history through a down migration.

## Automated verification

Run `make lint test build` for templ and sqlc generation, format checks, Go vet, race tests, asset compilation and binaries. Run migrations and fixtures only in disposable PostgreSQL databases. `TestPlatformPostgres` requires a separate seeded database with no preexisting administrator. Foundation CI runs the complete HTTP PostgreSQL suite, migrations, finance reconciliation, populated restore and Chromium checks at 320, 375, 768, 1024 and 1440 pixels, including JavaScript-disabled paths and screenshots. Docker CI checks a clean Compose installation and upgrades a populated image built from the owner-merged M12 commit. It compares accounts, category associations, messages, orders, ledger rows and media through container replacement. Both workflows use simulated providers.
