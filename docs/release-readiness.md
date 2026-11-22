# Private marketplace release-readiness report

**Review date** 22 November 2026

**Environment** Isolated PostgreSQL 17 and loopback web and worker processes, with synthetic data and simulated providers

**Status** Sandbox hardening and recovery rehearsal. Private Tor transport verified with Chromium. Tor Browser, formal accessibility sign-off, and deployment and payment approvals are open.

## Verification evidence

| Area | Reproduced check | Result |
| --- | --- | --- |
| Reproducible build | `make fmt` and `make lint test build` | Go and templ formatting, vet, race tests, packaged web and worker binaries. |
| Dependencies | `make audit` | The inherited scan reported 39 reachable advisory findings. After updating Go to 1.26.11, pgx to 5.9.2, and x/text to 0.41.0, govulncheck reported zero reachable findings and flagged 21 at module level outside the imported packages. npm audit reported zero findings. This is a point-in-time scan. |
| Database regressions | `make migrate && make rollback && make migrate && make fixtures && make fixtures` followed by the eight PostgreSQL integration suites with `-race` | Schema round-trip and fixture replay. Identity and roles, catalog, seller, messaging, orders, reviews, finance and admin suites cover resource ownership, private downloads, immutable purchase events and journal replay. |
| Restore | `make reconcile`, `make restore-drill`, then the Orders, Finance and Admin PostgreSQL suites against the restore | Restored all public table rows including private fulfillment fixtures and audit history. Repeat suites and reconciliation passed. Same-database and populated-target safeguards rejected invalid targets. |
| Finance | `make reconcile` before and after restore and replay | Sealed, balanced journals. Seller buckets, payout reservations, separate holds, reconciliation amounts, refunds and paid-order event consistency verified using synthetic data. |
| Browser | `bash scripts/smoke.sh` in CI, plus local targeted Chromium checks | Responsive viewport coverage at 320, 375, 768, 1024 and 1440 px. The targeted suite checks skip-link focus, light/dark body text contrast, reduced-motion styles, responsive dispute records, accessible form labels and no-JavaScript actions. CI stores screenshots. |
| Private Tor transport | `python scripts/tor_smoke.py` with Tor 0.4.9.15 and a loopback-only Chutney v3 onion network | Chromium completed no-JavaScript discovery, registration, messaging, simulated payment and a buyer-only download through the private Tor SOCKS connection. The onion referrer policy now preserves the origin on same-origin form submissions. Go regression tests confirm rejection of opaque and cross-site origins. |
| Slow connection | Throttled Chromium, JavaScript disabled, 200 ms emulated latency, 1.6 Mbps down and 0.75 Mbps up | Discovery, keyboard navigation, registration, simulated checkout, and access to the private download link on refresh. All observed requests stayed on the local origin. This measures application behavior under an emulated connection. |

The PostgreSQL suite uses the corresponding `MARKETPLACE_<AREA>_TEST_DATABASE_URL` variables with `go test -mod=readonly -race ./internal/http -run 'Test(Admin|Finance|Reviews|Orders|Messages|Seller|Catalog|Identity)Postgres$' -v`. Use a disposable database. See [isolated operations](operations.md) for restore and incident procedures.

References [Go vulnerability management](https://go.dev/doc/security/vuln/), [Go releases](https://go.dev/dl/), and [WCAG 2.2](https://www.w3.org/TR/WCAG22/).

## Before a release decision

| Pending | Impact |
| --- | --- |
| Tor Browser and selected ingress | The private test used Chromium over local Tor circuits. Validate actual Tor Browser behavior, cookies, forms, access boundaries, and timeouts on the selected ingress before external access. |
| Accessibility sign-off | Browser checks cover representative pages, controls, viewport sizes, themes and motion preferences. Assistive-technology testing and an owner-approved WCAG review are pending. |
| Production configuration and recovery | The rehearsal used synthetic data and local database roles. Review ingress, reverse proxy and session-cookie configuration, minimum database grants, encrypted backup storage, restore permissions, recovery objectives, and incident ownership on the chosen infrastructure. |
| Worker and provider operations | The worker records heartbeat and sweeps unpaid orders. The sandbox persists notices and reconciliations, with payout observation and reconciliation driven by finance staff. Provider-backed jobs and any policy-driven cleanup require approved retention and provider contracts. |
| Legal, policy and live payments | Decide the X01–X07 entries in [open decisions](open-decisions.md). The [policy draft](policy-review.md) lists scope, moderation, dispute and retention decisions. The application remains a private simulator. |

The [threat model](threat-model.md) records residual risks and the boundaries reviewed. M9 runs on schema version 9 and keeps the existing simulated providers and review-stage policies.
