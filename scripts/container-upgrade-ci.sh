#!/usr/bin/env bash
set -euo pipefail

# This job follows the clean Compose check on a disposable Actions runner.
if [[ ${GITHUB_ACTIONS:-} != true || ${COMPOSE_PROJECT_NAME:-} != marketplace-ci ]]; then
  echo "Run this verification only inside the disposable marketplace-ci Actions stack."
  exit 1
fi
base_revision=ab45a203d55cd62f64ec7e973c09e4374480d4a2
image_next=${MARKETPLACE_IMAGE:?}
docker compose down
export COMPOSE_PROJECT_NAME=marketplace-upgrade-ci
export MARKETPLACE_IMAGE=marketplace:m12-ci
scratch=$(mktemp -d)
cleanup() {
  docker compose logs --no-color --tail 30 web migrate worker || true
  docker compose down --volumes || true
  git worktree remove "$scratch/prior" || true
  rm -rf -- "$scratch"
}
trap cleanup EXIT

git fetch --no-tags --depth=1 origin "$base_revision"
git worktree add --detach "$scratch/prior" "$base_revision"
docker build --tag "$MARKETPLACE_IMAGE" "$scratch/prior"
docker compose up -d --wait --wait-timeout 120

db() {
  docker compose exec -T postgres psql -X -v ON_ERROR_STOP=1 -U marketplace -d marketplace "$@"
}
version() {
  db -Atc 'SELECT max(version_id) FROM goose_db_version'
}
test "$(version)" = 12
db < "$scratch/prior/db/dev-fixtures.sql"
python - <<'PY'
from pathlib import Path
from playwright.sync_api import sync_playwright
from scripts.browser_smoke import verify_seller, verify_profiles, verify_messages, verify_orders

base = "http://127.0.0.1:8080"
with sync_playwright() as playwright:
    browser = playwright.chromium.launch()
    listing = verify_seller(browser, base)
    verify_messages(browser, base, listing)
    avatar = verify_profiles(browser, base)
    verify_orders(browser, base)
    Path("bin/smoke/upgrade-listing-url").write_text(listing + "/image")
    Path("bin/smoke/upgrade-avatar-url").write_text(avatar)
    browser.close()
PY

docker compose stop web worker
db -At <<'SQL' > "$scratch/snapshot.sql"
SELECT format(
  'SELECT %L, count(*), md5(coalesce(string_agg(digest, %L ORDER BY digest), %L)) FROM (SELECT md5(to_jsonb(t)::text) AS digest FROM %I t) records;',
  name, '', '', name)
FROM unnest(ARRAY['users','listings','listing_images','user_profiles','role_assignments', 'role_change_events',
    'conversations','orders','order_items','shipping_details','digital_entitlements',
    'payment_observations','journal_entries','journal_lines']) AS names(name);
SELECT $$SELECT 'categories', count(*), md5(coalesce(string_agg(digest, '' ORDER BY digest), ''))
FROM (SELECT md5(to_jsonb(c)::text) AS digest FROM (SELECT slug,name,parent_slug FROM categories) c) records;$$;
SQL
db -At < "$scratch/snapshot.sql" > "$scratch/prior.snapshot"
docker compose exec -T postgres pg_dump -Fc -U marketplace -d marketplace > "$scratch/marketplace.dump"
test -s "$scratch/marketplace.dump"

export MARKETPLACE_IMAGE=$image_next
docker compose up -d --wait --wait-timeout 120
target=$(find db/migrations -name '*.sql' -printf '%f\n' | sort | tail -1 | cut -d_ -f1 | sed 's/^0*//')
test "$(version)" = "$target"
test "$(docker inspect --format '{{.State.ExitCode}}' "$(docker compose ps -aq migrate)")" = 0
test "$(docker compose port web 8080)" = 127.0.0.1:8080
docker compose exec -T worker /app/container-health worker
db -At < "$scratch/snapshot.sql" > "$scratch/upgraded.snapshot"
cmp "$scratch/prior.snapshot" "$scratch/upgraded.snapshot"
db < scripts/check-finance.sql
for kind in listing avatar; do
  url=$(cat "bin/smoke/upgrade-$kind-url")
  curl -fsS "$url" > "$scratch/$kind.before"
done

docker compose down
docker compose up -d --wait --wait-timeout 120
for kind in listing avatar; do
  url=$(cat "bin/smoke/upgrade-$kind-url")
  curl -fsS "$url" > "$scratch/$kind.after"
  cmp "$scratch/$kind.before" "$scratch/$kind.after"
done
echo "M12 → M13 upgrade preserved application records, ledger balances, listing images and avatars."
