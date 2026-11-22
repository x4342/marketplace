# Private Marketplace

A private Go marketplace built to the [V5 blueprint](docs/v5-blueprint.md). The current foundation serves a PostgreSQL-backed service overview, a background-worker heartbeat, and a responsive light/dark shell.

## Run locally

Install Go 1.25+, Node.js 22+, Docker Compose, Make, and PostgreSQL client tools.

```sh
cp .env.example .env
set -a
. ./.env
set +a
make setup
docker compose up -d --wait postgres
make migrate
make build
./bin/web
```

From a second terminal with the same environment variables, run `./bin/worker`. Open <http://127.0.0.1:8080>. The worker records a heartbeat immediately and every 15 seconds. The overview shows it as inactive after 45 seconds without an update. Light and Dark preferences persist across reloads with or without JavaScript. Refresh status uses a full page GET when JavaScript is disabled.

## Verify

```sh
make fmt
make lint test
```

On the isolated fixture database, before starting the worker, validate rollback/reapply and create a fixture.

```sh
make rollback
make migrate
make fixtures
```

`GET /health/live` checks the web process. `GET /health/ready` returns 503 if PostgreSQL or the required schema is unavailable. For browser and process verification, run the same smoke suite used in CI against a fresh fixture database with the web and worker stopped.

```sh
python -m pip install playwright==1.55.0
python -m playwright install chromium
bash scripts/smoke.sh
```

The suite checks local assets, readiness, heartbeat changes, HTMX refresh, no-JavaScript theme persistence, and layouts at five widths.

## Configuration and privacy

`.env.example` and Compose contain local fixture credentials. Keep real credentials and private data out of source, fixtures, logs, and build artifacts. The local HTTP listener and the Compose host port bind to loopback. Runtime logs report process and service failures without request bodies, cookies, or driver errors.

See the [milestone tracker](docs/implementation-plan.md), [engineering decisions](docs/decisions.md), and [owner decisions](docs/open-decisions.md).

## Local accounts

Use `/register` with a username and password, then save the recovery codes displayed after enrollment. The password recovery form uses one code, invalidates existing sessions and codes, and shows a new set. Account and recovery routes work as standard HTML forms.

For the M1 PostgreSQL tests, point the explicit test variable at an isolated development database after migration.

```sh
MARKETPLACE_IDENTITY_TEST_DATABASE_URL="$DATABASE_URL" go test -race ./internal/http -run TestIdentityPostgres -v
```

For development role checks, use a test database and assign `moderator` or `finance_admin` to a known fixture account in `role_assignments`. Both roles are checked on each protected request. Production role provisioning and administrative queues are later work.

## Local discovery

Run `make fixtures` after migration to load the catalog examples. Open `/discover` for search, category and availability filters, price sorting and published storefronts. Sign in to save a listing from its detail page and find it later under Saved listings.

Price comparisons use the dated simulated USD/EUR rate in migration 00003. Listing prices remain in their original currencies. Review-backed sorting and filtering require legitimate review data. Listing image uploads and seller listing management belong to M3.
