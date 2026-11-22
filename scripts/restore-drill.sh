#!/usr/bin/env bash
set -euo pipefail
umask 077

: "${DATABASE_URL:?Set DATABASE_URL to a populated synthetic test database}"
: "${RESTORE_DATABASE_URL:?Set RESTORE_DATABASE_URL to a new, empty local database}"
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."

# Require explicit loopback PostgreSQL URLs. Reject connection overrides in URL options.
python3 - <<'PY'
import ipaddress
import os
from urllib.parse import parse_qs, urlsplit

source = urlsplit(os.environ["DATABASE_URL"])
target = urlsplit(os.environ["RESTORE_DATABASE_URL"])
for url in (source, target):
    try:
        valid = (
            url.scheme in {"postgres", "postgresql"}
            and ipaddress.ip_address(url.hostname).is_loopback
            and 0 < url.port < 65536
            and url.path.startswith("/")
            and len(url.path) > 1
            and not url.fragment
            and set(parse_qs(url.query, keep_blank_values=True)) <= {"sslmode"}
        )
    except (ValueError, TypeError):
        valid = False
    if not valid:
        raise SystemExit("Use explicit loopback PostgreSQL URLs with a numeric port and database name.")
if source.path == target.path:
    raise SystemExit("Use a separate restore database name.")
PY

source_db=(psql --dbname="$DATABASE_URL" --no-psqlrc --no-align --tuples-only --set=ON_ERROR_STOP=1)
target_db=(psql --dbname="$RESTORE_DATABASE_URL" --no-psqlrc --no-align --tuples-only --set=ON_ERROR_STOP=1)
source_name=$("${source_db[@]}" -c 'SELECT current_database()')
target_name=$("${target_db[@]}" -c 'SELECT current_database()')
if [[ "$source_name" == "$target_name" ]]; then
    printf '%s\n' 'Use a separate restore database name.' >&2
    exit 1
fi
if [[ $("${target_db[@]}" -c "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public'") != 0 ]]; then
    printf '%s\n' 'The restore database must have an empty public schema.' >&2
    exit 1
fi
if [[ $("${source_db[@]}" -c "
    SELECT EXISTS (SELECT 1 FROM reconciliation_items)
       AND EXISTS (SELECT 1 FROM orders WHERE state='refunded')
       AND EXISTS (SELECT 1 FROM shipping_details)
       AND EXISTS (SELECT 1 FROM digital_entitlements)
       AND EXISTS (SELECT 1 FROM admin_audit_events)
") != t ]]; then
    printf '%s\n' 'Seed source fixtures with reconciled payouts, refunds, private fulfillment, and audit history.' >&2
    exit 1
fi

# Synthetic data only. The drill does not retain the archive or publish data checksums.
drill_dir=$(mktemp -d)
trap 'rm -rf -- "$drill_dir"' EXIT
pg_dump --dbname="$DATABASE_URL" --format=custom >"$drill_dir/marketplace.dump"
pg_restore --dbname="$RESTORE_DATABASE_URL" --single-transaction --exit-on-error <"$drill_dir/marketplace.dump"

snapshot() {
    "$@" --quiet <<'SQL'
BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL TIME ZONE 'UTC';
SELECT format(
    'SELECT %L, count(*), md5(coalesce(string_agg(digest, %L ORDER BY digest COLLATE "C"), %L)) FROM (SELECT md5(to_jsonb(r)::text) AS digest FROM %I.%I r) rows',
    tablename, '', '', schemaname, tablename)
FROM pg_tables WHERE schemaname = 'public' ORDER BY tablename COLLATE "C"
\gexec
COMMIT;
SQL
}
snapshot "${source_db[@]}" >"$drill_dir/source"
snapshot "${target_db[@]}" >"$drill_dir/target"
if ! cmp -s "$drill_dir/source" "$drill_dir/target"; then
    printf '%s\n' 'Restore rows differ from source. Quiesce synthetic writers before retrying.' >&2
    exit 1
fi

"${target_db[@]}" --file=scripts/check-finance.sql
printf '%s\n' 'Restore verified. All public table snapshots and finance invariants match.'
