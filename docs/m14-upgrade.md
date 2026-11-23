# M14 credentials, search and direct messaging

## Routes and privacy

- New registration accepts 3–20 ASCII letters or digits, storing lowercase. Existing usernames, including underscore and long names, still work in sign-in, recovery, profile links, authorship, and username search. Migration 00014 installs a write-time constraint for inserts and username changes. No row or credential hash is rewritten. M12's reserved administrator name and local bootstrap still apply.
- New and reset passwords accept 8–128 Unicode code points. Space, symbols and Unicode are supported. Verification takes the exact submitted bytes. New passwords reject Unicode control characters. Existing hashes can still be verified with their original bytes. The browser has no UTF-16 password length attributes. The server bounds the password bytes and the 4 KiB account form.
- The header owns the global keyword field. `GET /discover` uses products by default, including older URLs with only `q`. `GET /discover?search=users&q=alice` searches normalized username prefixes, using migration 00015's C-collation index, at 24 public results per page. It ignores product facets. Search shows public names, avatars and biography excerpts and excludes restricted profiles.
- Authenticated members start direct conversations with a CSRF-protected `POST /messages/direct`, available from profiles and user results. **New message** in Messages opens Users mode. `GET /messages` opens the most recent conversation, while `/messages/{id}` reopens the selected thread and preserves message notification links.
- Migration 00016 represents direct conversations with nullable listing context, a canonical unordered pair and a partial unique index. It adds the direct start audit timestamp and initiator, without changing existing conversation rows, IDs, message bodies, reports, blocks, read cursors or notifications. Listing-linked threads and verified purchase events stay separate. The database requires a matching listing thread, order snapshot and verified payment observation for newly inserted purchase events.
- A block applies within its conversation. It pauses new messages in both directions. A direct block does not alter listing threads or order notifications. Account restrictions prevent direct initiation and pause sending in affected threads. Existing participants retain read access. Only participants can read conversation bodies or previews. Moderator reports retain the approved, audited access boundaries.
- The app allows ten new direct starts and thirty sent messages per account per rolling minute. It serializes starts and sends per account. Existing direct threads can be reopened without consuming a new start. The app uses standard form posts and keeps the composer outside the polling fragment. JavaScript remembers only sidebar expansion and warns before abandoning a draft. With JavaScript disabled, the native selector and all actions still work. Finish or copy unsent text before following a different conversation in that mode.

## Safe local upgrade

Use the [Arch and Portainer runbook](platform-administration.md) for the exact host commands to stop writers, make and validate a private PostgreSQL archive, build an approved image from a detached worktree, and redeploy the existing stack. Preserve the existing database password, project name and PostgreSQL volume. The same database volume stores all media, conversations and financial history. The web port is still published only on loopback.

Before deploying, optionally inventory legacy usernames in the local PostgreSQL console. This is a read-only check. Retain the resulting names privately.

```sql
SELECT count(*) FROM users WHERE username COLLATE "C" !~ '^[a-z0-9]{3,20}$';
```

After redeployment, look for schema version 16 and a successful migrator exit. Check `/health/live`, `/health/ready` and the worker heartbeat. Sign in with an existing account, confirm Discover opens in Products mode, search for an eligible member, and test a direct message with a consenting local test account. Open an existing listing conversation and check its prior messages and any verified purchase event. For rollback, restore the pre-upgrade archive into a fresh volume and redeploy the prior image using the exact [archive recovery procedure](platform-administration.md#rollback-with-the-verified-archive). Populated direct threads cause Goose to refuse the downgrade.

## Automated checks

Run `make fmt` and `make lint test build`. Run Foundation CI or its component commands against a disposable PostgreSQL 17 database. The workflow runs the legacy-role and username grandfathering upgrade tests, the HTTP credential and direct-message tests, the existing application tests, reconciliation and a populated restore. `scripts/smoke.sh` runs the Chromium suite, including viewport checks and screenshots for search and Messages. The dedicated Docker workflow builds and upgrades the owner-approved M13 image with populated fixtures, compares application, conversation and ledger records across the upgrade, and checks media after recreation.
