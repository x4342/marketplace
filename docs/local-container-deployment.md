# M10 — Local Container Deployment

This runbook targets **Docker Standalone** managed by Portainer on an Arch Linux workstation. Use the machine running Docker to open the marketplace at [http://127.0.0.1:8080](http://127.0.0.1:8080). Checkout, refunds, and payouts use the existing simulation providers.

## Prepare Arch Linux and the image

With Docker and Portainer already installed, confirm the daemon, CLI and Compose plugin work. Use the package installation command only if the packages are missing or the Docker server is older than 28.

```sh
sudo pacman -Syu --needed docker docker-compose git curl openssl
sudo systemctl enable --now docker.service
sudo docker version --format '{{.Server.Version}}'
sudo docker compose version
sudo ss -H -ltn 'sport = :8080'
```

Use Docker Engine **28 or newer**, the default bridge routing mode, and an unused host port 8080. An empty `ss` result means there is no ordinary TCP listener on that port. Check `sudo docker ps` for published ports too. Keep Portainer on its existing management endpoint. [Arch Linux Docker](https://wiki.archlinux.org/title/Docker) · [Docker loopback publication](https://docs.docker.com/engine/network/port-publishing/)

For an image built locally, authenticate Git to read the private repository, then run these commands against the same Docker daemon that Portainer manages. Use the review branch to test M10. Select the approved revision on `main` once it contains M10.

```sh
git clone https://github.com/x4342/marketplace.git
cd marketplace
git switch --track origin/feature/m10-local-container
revision=$(git rev-parse HEAD)
image="marketplace:sha-$revision"
sudo docker build --tag "$image" .
printf 'MARKETPLACE_IMAGE=%s\n' "$image"
openssl rand -hex 32
```

Save the generated password in your password manager. Paste the 64 hexadecimal characters exactly as printed. The password is part of the internal database URI and initializes the PostgreSQL volume. Keep the same password on redeploy. Changing the Portainer variable alone does not alter an initialized PostgreSQL role. Portainer and trusted Docker administrators can read container environment variables.

## First deployment in Portainer

1. Open your existing Portainer URL, select your **local Docker Standalone** environment, then go to **Stacks → Add stack**.
2. Name the stack `marketplace`. Select **Git repository** and enable **Authentication**. Set the repository URL to `https://github.com/x4342/marketplace.git`, reference to `refs/heads/feature/m10-local-container`, and Compose path to `compose.portainer.yaml`. Use `refs/heads/main` once the approved deployment changes are on main.
3. Enter your GitHub username and a token with read access to this repository. A fine-grained token scoped to this repository with **Contents: Read-only** is sufficient for cloning. Keep Git polling and automatic updates disabled for manual migration review.
4. Under **Environment variables**, set `MARKETPLACE_IMAGE` to the exact image reference printed by the build or the private GHCR digest. Set `MARKETPLACE_DB_PASSWORD` to your generated password. Leave `MARKETPLACE_DB_VOLUME` unset for the default `marketplace_marketplace-data` volume.
5. Click **Deploy the stack**. For locally built images, leave any force-pull option disabled. Wait for `postgres` to become healthy, `migrate` to exit with code 0, and `web` and `worker` to become healthy. The migration uses the checked-in SQL and serializes concurrent runs with a PostgreSQL lock. On migration failure, inspect its logs before starting the app.
6. Open [http://127.0.0.1:8080](http://127.0.0.1:8080). Register your local account. Discovery starts empty until you publish a listing or load the optional sandbox fixtures.

The web listener uses `0.0.0.0:8080` only inside Docker through the explicit bridge opt-in. The published host endpoint is IPv4 loopback only. The database and worker have no host port mappings. Keep the stack off host networking and external reverse proxies. [Portainer Git deployment](https://docs.portainer.io/user/docker/stacks/add)

```sh
curl -fsS http://127.0.0.1:8080/health/ready
worker=$(sudo docker ps -q --filter label=com.docker.compose.project=marketplace --filter label=com.docker.compose.service=worker)
test -n "$worker" && sudo docker exec "$worker" /app/container-health worker
sudo docker ps --filter label=com.docker.compose.project=marketplace
```

### Optional private GHCR image

After M10 is on the default branch, the owner can manually run **Actions → Publish private marketplace image → Run workflow** on `main`. The workflow builds Linux amd64 and publishes the revision as `ghcr.io/x4342/marketplace:sha-COMMIT_SHA`. Confirm any existing package of that name is private before running the workflow and verify package visibility and repository links afterward. Use the digest reference printed in the workflow summary for `MARKETPLACE_IMAGE`. A local build works without publication.

In Portainer, add a GitHub or custom registry for `ghcr.io` under **Registries → Add registry**. Use your GitHub username and a **classic PAT** with `read:packages` plus access to the private package. Enable registry access for the local endpoint if your Portainer edition uses registry access controls. Git clone authentication and GHCR pull authentication are separate. For a shell pull or a local diagnostic, `sudo docker login ghcr.io --username YOUR_USERNAME` prompts for the package token. [Portainer registries](https://docs.portainer.io/admin/registries/add) · [GitHub Container registry authentication](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry)

## Fixtures and logs

Fixtures are optional synthetic catalog data. The stack never imports them during startup. From your checked-out repository, run the fixture command only when you intend to seed the local sandbox.

```sh
postgres=$(sudo docker ps -q --filter label=com.docker.compose.project=marketplace --filter label=com.docker.compose.service=postgres)
test -n "$postgres" && sudo docker exec -i "$postgres" psql -X -v ON_ERROR_STOP=1 -U marketplace -d marketplace < db/dev-fixtures.sql
```

Use **Stacks → marketplace → select a container → Logs** in Portainer. Check `postgres` for readiness, `migrate` for the applied version or a failure, and `worker` for heartbeat and expiry-sweep problems. Portainer also shows container health and the migrator exit code. [Portainer stack inspection](https://docs.portainer.io/user/docker/stacks/edit)

```sh
sudo docker ps -a --filter label=com.docker.compose.project=marketplace
sudo docker logs --tail 100 CONTAINER_ID
```

## Upgrade and rollback

1. Record the currently deployed Git reference and image reference. Read any new migrations before upgrading. Prepare the exact new image with a local build or private GHCR.
2. Stop `web` and `worker` in Portainer and leave PostgreSQL running. Create a private pre-upgrade backup. Verify the command succeeded before continuing.

   ```sh
   umask 077
   postgres=$(sudo docker ps -q --filter label=com.docker.compose.project=marketplace --filter label=com.docker.compose.service=postgres)
   backup="$HOME/marketplace-before-$(date +%Y%m%d-%H%M%S).dump"
   if [ -n "$postgres" ] && sudo docker exec "$postgres" pg_dump -Fc -U marketplace -d marketplace > "$backup" && [ -s "$backup" ]; then
     printf 'Backup saved to %s\n' "$backup"
   else
     printf '%s\n' 'Backup failed. Stop the upgrade and inspect the PostgreSQL container.' >&2
   fi
   ```

3. In **Stacks → marketplace**, update `MARKETPLACE_IMAGE` and the Git reference if needed. Use **Pull and redeploy** for the Git stack. Keep the saved database password and volume. Verify readiness, the migrator exit code, and the worker heartbeat before resuming use.
4. For a schema-compatible rollback, set the previous image and Compose revision in Portainer and redeploy. Keep the original database volume. If readiness fails or the schema changed incompatibly, stop web and worker again. Restore the pre-upgrade archive to a **fresh volume** and use the previous approved image with it. Leave the upgraded volume available for investigation. The archive contains only data committed before it was taken.

To prepare a fresh recovery volume, create a private env file named `recovery.env` with mode 0600. Put the previous `MARKETPLACE_IMAGE`, the saved `MARKETPLACE_DB_PASSWORD` and a new unused name such as `MARKETPLACE_DB_VOLUME=marketplace_recovered_20261123` in it. Use the Compose file for the previous compatible release. Start PostgreSQL alone, restore the archive, then stop the temporary recovery project while retaining its volume.

```sh
sudo docker compose --env-file recovery.env -p marketplace-recovery -f compose.portainer.yaml up -d --wait postgres
sudo docker compose --env-file recovery.env -p marketplace-recovery -f compose.portainer.yaml exec -T postgres \
  pg_restore --single-transaction --exit-on-error -U marketplace -d marketplace < "$backup"
sudo docker compose --env-file recovery.env -p marketplace-recovery -f compose.portainer.yaml down
```

In Portainer, set `MARKETPLACE_DB_VOLUME` to the recovered volume name and `MARKETPLACE_IMAGE` to the previous compatible image, then redeploy and check health. Keep the archive protected because it can contain private account and marketplace data. No automatic down migration runs during application rollback.


## Listing image storage

M11 stores listing and draft JPEGs in PostgreSQL. The existing `marketplace_marketplace-data` volume contains the images and the application's other data. Keep the same volume on upgrades. The app image stays read-only and the stack uses the existing port mappings. Include the database volume in the documented backup and recovery process.

Migration `00010` raises existing JPEG column limits without rewriting media. If a new upload exceeds the former 3 MiB limit, PostgreSQL refuses a schema downgrade to that limit. Keep version 10 in place for a compatible app rollback or use the pre-upgrade backup procedure when the older schema is required. See the [media guide](listing-media.md).


## M12 profiles and administrator provisioning

The existing PostgreSQL volume stores profile biographies, sanitized avatars, and role history. Review the [legacy role and bootstrap guide](authorization.md) before upgrading. Bootstrap is a separate local interactive action after migration, and the normal stack startup never assigns an administrator.

## M13 upgrade

For M13, use the [administrator and upgrade guide](platform-administration.md) for the exact Arch Linux commands, schema-13 readiness checks, and archive recovery. The Compose project and PostgreSQL volume remain the same across the image replacement. System status moves into `/admin` and container health checks continue to use the existing probes.
