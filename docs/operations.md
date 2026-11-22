# Isolated operations and incident rehearsal

## Backup, restore and reconciliation

Use a populated synthetic database, a separate empty local PostgreSQL 17 database, and matching client tools. Populate the source with the Orders, Finance and Admin PostgreSQL suites. They create physical and digital fulfillment, refunds, reconciled payouts, and case history. Quiesce the web and worker before using the source for comparison. Ensure the database role can restore ownership and grants from the source archive to the target.

```sh
make reconcile
make restore-drill
DATABASE_URL="$RESTORE_DATABASE_URL" make reconcile
```

`DATABASE_URL` and `RESTORE_DATABASE_URL` must identify distinct databases in explicit loopback PostgreSQL URLs. The drill checks for an empty public schema before writing. It makes a temporary PostgreSQL custom archive with private filesystem permissions, restores in a single transaction, and compares sorted row counts and digests across all public tables. It discards the archive and comparison data at exit. Check finance and order behavior against the restored database before pointing any sandbox application process at it. GitHub Actions repeats order, finance and admin flows and reconciles again after replaying those flows.

`make reconcile` is a read-only check for sealed and balanced journals, nonnegative seller buckets, payout reservations, separate holds, settlement amounts, and correspondence between paid journals and private purchase events. A failed check requires investigation. Keep ledger and audit history intact. Use approved compensating actions after reviewing the original operation and related evidence.

For schema changes, rehearse both an empty migration round-trip and an upgrade using a copy of populated data where appropriate. Migrations with immutable histories may refuse rollback. Recover from a verified archive or make a forward correction after review. This isolated drill exercises the PostgreSQL database and schema. Production backup encryption, key custody, copies, restore access, recovery objectives, and failure-domain separation need owner review and a separate rehearsal on the selected infrastructure.

PostgreSQL references [pg_dump](https://www.postgresql.org/docs/17/app-pgdump.html) and [pg_restore](https://www.postgresql.org/docs/17/app-pgrestore.html).

## Incident response

1. Log the first observed time in UTC and a short description in a private incident record. Record the affected route or role, counts, and opaque operation IDs when they aid diagnosis. Keep addresses, passwords, cookies, recovery codes, chat content, and key material out of incident notes and routine logs.
2. Escalate to the owner. Contain through the isolated runner or entry point. For suspected account abuse, revoke the affected session or role using the approved operator path. For financial mismatches, pause the affected workflow and preserve the original events and journals.
3. Record the application revision, migration version, and exact failing check. Preserve scoped logs or database evidence under restricted access. Compare the same check on an isolated restore before deciding whether the issue is data, code, configuration, or provider-related.
4. Apply a reviewed forward fix, restore to a fresh target if required, and rerun authorization checks, finance reconciliation, health checks and the affected user journey before reopening access. Record unresolved impact, required follow-up, and how access was restored.

Escalation contacts, evidence handling, notification obligations, and authority to reopen are release decisions. Avoid collecting extra private data while those decisions are open.

## Proposed production configuration review

| Area | Current sandbox configuration | Required before any external preview or live release |
| --- | --- | --- |
| Web ingress and headers | Numeric loopback listener, request timeouts, private responses with `no-store`, same-origin CSP, no cross-site referrers and MIME sniffing disabled | Approve Onion or regular web access, TLS termination, trusted host and proxy rules, cookie security in the actual browser, and resource ceilings. The current HTTPS cookie flag uses direct TLS state. |
| Database | Local PostgreSQL, synthetic fixture credentials, migrations and application share the test role | Use separate migration/operator/app roles, review least privilege and TLS, set encrypted backup policy, and prove recovery on the chosen infrastructure. |
| Revision and health | Go web and worker services, readiness and heartbeat endpoints, local asset build | Pin an approved maintained toolchain, scan supported dependencies, keep internal health endpoints restricted as appropriate, and choose alert routing and deployment ownership. |
| Secrets | Local environment variables, simulator credentials, no wallet keys | Provision a protected secret manager and least-privilege provider identities only after separate approval. |
| Financial operations | Mock quote and verification, TEST payouts, sandbox dispute buttons | Approve provider, legal scope, settlement/reconciliation rules, and staff control procedures. |

## Private Tor browser smoke

For an isolated Chutney `hs-v3-min` network, point the disposable onion service at the loopback sandbox app. Keep authorities, relays, directory and SOCKS listeners on loopback, use local test authorities and disable public fallbacks and exits. Use only synthetic accounts and the seeded catalog. Stop the test network after the run.

```sh
MARKETPLACE_TOR_ORIGIN="http://$TEST_ONION_HOSTNAME" \
MARKETPLACE_TOR_PROXY="socks5://127.0.0.1:9006" \
python scripts/tor_smoke.py
```

The optional smoke uses Chromium over the private Tor client's SOCKS port, with JavaScript disabled. An onion response uses a same-origin referrer policy so standard browser form posts carry their origin to the marketplace. Normal cross-origin protections and private form tokens continue to apply.

References [Tor Project onion service documentation](https://community.torproject.org/onion-services/setup/) and [Fetch Standard origin header rules](https://fetch.spec.whatwg.org/#origin-header).
