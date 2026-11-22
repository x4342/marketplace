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

## Seller listing lifecycle

Sign in and open **Sell** from your account. Start a private draft, save partial details, preview at desktop and mobile widths, and publish when the required fields are complete. Editing a published listing creates an independent draft. Pausing or removing it hides the listing and its media from the public catalog.

Uploads accept JPEG and PNG up to 3 MB and 2048 pixels per side. The server decodes and re-encodes the image as JPEG. Vacation mode displays an away status on the seller’s published listings. Checkout enforcement belongs to the later order workflow.

Run the seller integration tests against a migrated disposable database with catalog fixtures.

```sh
MARKETPLACE_SELLER_TEST_DATABASE_URL="$DATABASE_URL" go test -race ./internal/http -run TestSellerPostgres -v
```

## Private listing messages

Sign in as a buyer and choose **Message seller** on a published listing. Open **Messages** and **Notifications** from your account. Conversations and notification links are restricted to their participants and recipients respectively. Messages are plain text. The standard HTML forms support sending, marking history as read, blocking, and reporting with JavaScript disabled.

Blocking applies to the current conversation. Reports are persisted for the later moderation queue. The purchase-event renderer and append-only schema are present for verified checkout in M5.

```sh
MARKETPLACE_MESSAGES_TEST_DATABASE_URL="$DATABASE_URL" go test -race ./internal/http -run TestMessagesPostgres -v
```

## Simulated orders and fulfillment

Sign in as a buyer and choose **Simulated checkout** on an available listing. Physical checkout accepts fictional address details for the listing’s shipping regions. Checkout commits the order and listing snapshot before asking for a quote. The buyer can simulate a matching payment or cancel while it is unpaid. The worker expires overdue unpaid orders and restores any reserved stock.

Sellers can dispatch physical orders with an optional test tracking reference or submit private manual delivery text. Automatic listings need a plain UTF-8 test document set under **Sell**. Paid buyers can download the version saved at checkout. For a seeded download, use **Workshop inventory sheets** after running `make fixtures`.

The sandbox uses one unit per order, a 20-minute mock quote, 100 test units per accounting cent, and a 48-hour display deadline for manual delivery. Automatic files are limited to 64 KiB of plain UTF-8 for this simulation. Live assets, payment confirmation, retention, expiry and dispute terms remain owner decisions. Buyer problem reports reach the order timeline. Full dispute review follows in M8.

```sh
MARKETPLACE_ORDERS_TEST_DATABASE_URL="$DATABASE_URL" go test -race ./internal/http -run TestOrdersPostgres -v
```

Migration 00006 rolls back an empty order schema. Once order history exists, use a backup restore or a forward migration. Seller stock edits represent units currently available for new orders. Use a disposable database for this suite. It leaves its synthetic order records intact to respect the database’s append-only snapshots and events.
