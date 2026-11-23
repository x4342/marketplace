# Private Marketplace

A private Go marketplace built to the [V5 blueprint](docs/v5-blueprint.md). The sandbox includes discovery, seller tools, private messaging and fulfillment, verified-order reviews, simulated checkout and payouts, and administrative moderation and disputes.

## Run in Portainer on Arch Linux

See [M10 — Local Container Deployment](docs/local-container-deployment.md) for the localhost-only stack, versioned images, setup, and recovery.

## Run locally for development

Install Go 1.26.11 or later. Local development also uses Node.js 22+, Docker Compose, Make, and PostgreSQL client tools.

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

From a second terminal with the same environment variables, run `./bin/worker`. Open <http://127.0.0.1:8080>. The worker records a heartbeat immediately and every 15 seconds. The overview shows it as inactive after 45 seconds without an update. The interface uses one dark theme across pages. Refresh status uses a full page GET when JavaScript is disabled.

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

The suite checks local assets, readiness, heartbeat changes, HTMX refresh, dark-only rendering without JavaScript, and layouts at five widths.

## Configuration and privacy

`.env.example` and Compose contain local fixture credentials. Keep real credentials and private data out of source, fixtures, logs, and build artifacts. The local HTTP listener and the Compose host port bind to loopback. Runtime logs report process and service failures without request bodies, cookies, or driver errors.

See the [milestone tracker](docs/implementation-plan.md), [engineering decisions](docs/decisions.md), and [owner decisions](docs/open-decisions.md).

## Local accounts

Use `/register` with a username and password, then save the recovery codes displayed after enrollment. The password recovery form uses one code, invalidates existing sessions and codes, and shows a new set. Account and recovery routes work as standard HTML forms.

For the M1 PostgreSQL tests, point the explicit test variable at an isolated development database after migration.

```sh
MARKETPLACE_IDENTITY_TEST_DATABASE_URL="$DATABASE_URL" go test -race ./internal/http -run TestIdentityPostgres -v
```

For development role checks, use a test database and assign `moderator` or `finance_admin` to a known fixture account in `role_assignments`. Both roles are checked on each protected request. Production role provisioning remains part of the deployment review.

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

The sandbox uses one unit per order, a 20-minute mock quote, 100 test units per accounting cent, and a 48-hour display deadline for manual delivery. Automatic files are limited to 64 KiB of plain UTF-8 for this simulation. Live assets, payment confirmation, retention, expiry and dispute terms remain owner decisions. Buyer problem reports open a dispute linked in the order timeline.

```sh
MARKETPLACE_ORDERS_TEST_DATABASE_URL="$DATABASE_URL" go test -race ./internal/http -run TestOrdersPostgres -v
```

Migration 00006 rolls back an empty order schema. Once order history exists, use a backup restore or a forward migration. Seller stock edits represent units currently available for new orders. Use a disposable database for this suite. It leaves its synthetic order records intact to respect the database’s append-only snapshots and events.

## Verified reviews and seller insights

Once a buyer confirms receipt on a paid order, they can submit one written review with a 1–5 star rating from the order page. Public reviews show the item title at purchase, star count, date and verified purchase status. Review author accounts and private order details are excluded from the public view. Seller averages and rating thresholds use at least three visible verified reviews. Listing and seller summaries exclude hidden reviews. A later refund adds a label to an existing public review.

Signed-in users can report a visible review. Content moderators decide review reports at **Account → Review moderation**, record a reason, and can uphold or reverse a hidden review when its author appeals. Notifications lead to the participant-only review page. Review moderation has its own queue. Listing, seller, and chat reports appear in Content moderation.

**Sell → Seller insights** shows the account’s aggregate listing page loads, orders, completed orders and gross simulated sales by original currency. Page loads are approximate, with no visitor identifiers stored. Gross sales exclude refunds and use completed verified orders before fees.

```sh
MARKETPLACE_REVIEWS_TEST_DATABASE_URL="$DATABASE_URL" go test -race ./internal/http -run TestReviewsPostgres -v
```

Use a disposable, migrated and seeded database. Migration 00007 refuses rollback once verified review history exists.

## Simulated earnings and payouts

Verified checkout posts a balanced accounting-currency journal and a separate TEST-unit payment journal. Seller earnings accrue as pending, move to available after buyer receipt in the sandbox, and move to held when a fulfillment problem is reported. The platform commission uses the proposed 5% merchandise-subtotal basis with integer half-up rounding. Views, gross sales, and ledger balances represent different measurements.

Open **Account → Earnings** to review currency-specific balances or request a simulated payout. The reservation is atomic. A different account with the `finance_admin` role can review, approve, process, check the deterministic provider, and reconcile from **Account → Payout review**. Approval records permission to proceed. Provider confirmation moves reserved funds to withdrawn history. Failed or stalled attempts keep the reservation until a deliberate retry or denial. Denial returns reserved funds to available unless finance has placed a separate payout hold. In that case, denial moves the reservation to held until finance lifts the hold.

Finance can record a full simulated refund or chain reversal for orders outside the dispute workflow through **Simulated payment adjustments**. Each posts compensating journals and leaves the original entries intact. If the corresponding seller bucket cannot cover the adjustment, it fails atomically for manual follow-up. Live refund, hold, release and shortfall policies remain open. No wallet address is collected. All finance rates, amounts and provider outcomes are synthetic sandbox inputs.

Migration 00008 backfills existing verified orders. It can roll back an empty financial schema and refuses to remove posted financial history. Run the integration test on an isolated database.

```sh
MARKETPLACE_FINANCE_TEST_DATABASE_URL="$DATABASE_URL" go test -race ./internal/http -run TestFinancePostgres -v
```

## Content moderation and order disputes

Buyers can select a complaint category and open a dispute from a paid order. Buyer and seller can submit text responses and evidence while the case is open or held. The case shows the item snapshot and whether dispatch, manual delivery, or download access were recorded. Shipping addresses, tracking references, and private fulfillment payloads stay on their original participant-only surfaces. An independent content moderator can explicitly maintain the hold, release seller earnings, or post a simulated refund. Release and refund use compensating journals and update both participants' order notices in the same transaction. Each participant can appeal a final decision. A different moderator may uphold it or refer it for further review. Referral leaves the posted outcome in effect pending an approved appeal policy.

Signed-in users can report a listing or seller from the listing page and report a conversation from the thread. **Account → Content reports** gives moderators a filterable queue. It shows the chat report and its reason without access to the conversation transcript. A moderator can dismiss a report, delist an item, restrict a conversation, or restrict an account from new checkout, publishing, and sending messages while allowing existing fulfillment. Affected users can appeal through **Account → Moderation decisions**. When a listing restriction is reversed, its seller can review it and resume publication.

Moderator and finance queues require separate role assignments. Finance staff can review payouts and place a separate hold on reserved funds. **Account → Administrative audit** shows permitted actions, reasons, actors, and timestamps. Moderator access to a restricted dispute record or chat report is recorded. Migration 00009 reconciles existing chat reports and administrative action histories. It can roll back an empty M8 schema and refuses rollback after dispute or moderation history exists. Use a backup restore or a forward migration for a populated installation.

```sh
MARKETPLACE_ADMIN_TEST_DATABASE_URL="$DATABASE_URL" go test -race ./internal/http -run TestAdminPostgres -v
```

## Sandbox release readiness

Run `make audit` to scan the application and frontend dependencies. For the populated restore drill and reconciliation commands, see [isolated operations](docs/operations.md). The [readiness report](docs/release-readiness.md) records verification evidence and remaining release decisions. The [threat model](docs/threat-model.md) and [policy draft](docs/policy-review.md) describe the current controls and outstanding approvals.
