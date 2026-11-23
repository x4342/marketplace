# Private Marketplace V5 implementation plan

The implementation agent owns engineering and verification. The repository owner owns the approvals in [open-decisions.md](open-decisions.md). Work is ordered by milestone. Completion requires the feature gate, actual evidence, and a traceable review branch or pull request.

| Milestone | Depends on | Acceptance gate | Status and evidence |
| --- | --- | --- | --- |
| M0 Foundation | Empty repo audited | Fresh configure, migrate up/down, generate, build, format, test, and run web/worker. Local assets, persistent theme, keyboard and no-JS shell across 320/375/768/1024/1440px. | Implemented and tested. [Green CI](https://github.com/x4342/marketplace/actions/runs/45059542225) covers format, vet, race tests, builds, migration up/down/up, fixture repeat, and Chromium light/dark, no-JS, keyboard and HTMX checks at the five target widths. Branch `feature/m0-foundation`. [Issue #1](https://github.com/x4342/marketplace/issues/1) · [PR #2](https://github.com/x4342/marketplace/pull/2). |
| M1 Identity | M0 | Username-only registration and login, one-use recovery, server sessions, owner checks, CSRF, separate moderator and finance permissions, negative and expiry tests. | Implemented and tested. [Issue #3](https://github.com/x4342/marketplace/issues/3) · [PR #4](https://github.com/x4342/marketplace/pull/4) · [Green CI](https://github.com/x4342/marketplace/actions/runs/45060187899). |
| M2 Catalog | M1 | Search/filter/storefront/favorite DB flows, mobile one and desktop two columns, visible sort, distinct availability, canonical URL/reset/history, stable paging, no fabricated ratings. | Discovery gate passed. [PR #6](https://github.com/x4342/marketplace/pull/6) · [Green CI](https://github.com/x4342/marketplace/actions/runs/45062779838) verifies PostgreSQL search, permissions, card favorites, no-JS keyboard drawer, and responsive discovery. Seller-uploaded media is tracked in M3. Review-backed facets await M6 data. |
| M3 Seller lifecycle | M2 | Save Draft at each stage, feedback and validation for upload, preview and publishing, listing ownership, vacation blocks new checkout and preserves existing obligations and snapshots. | Seller lifecycle verified. [PR #8](https://github.com/x4342/marketplace/pull/8) · [Lifecycle CI](https://github.com/x4342/marketplace/actions/runs/45063517152). Final upload progress and recovery passed local Chromium after fixing form endpoint selection. Checkout enforcement for vacation is scheduled in M5. |
| M4 Messaging | M1–M3 | Nonbuyers can message, participants only, block/report, unread and notification links, no-JS send, purchase events cannot be forged. | Messaging gate verified. [PR #10](https://github.com/x4342/marketplace/pull/10) · [Issue #9](https://github.com/x4342/marketplace/issues/9). Local PostgreSQL and race tests passed. Local Chromium verified mobile no-JS messaging, authenticated HTMX polling, and 320/375/768/1024/1440px layout. Purchase-event payment wiring follows in M5. |
| M5 Orders | M3–M4 | Immutable snapshot before mock quote, physical and digital fulfillment, protected download, exactly one private purchase event including cases without prior chats. | Complete in simulation. [PR #12](https://github.com/x4342/marketplace/pull/12) · [Passing CI](https://github.com/x4342/marketplace/actions/runs/45068044219) · [Issue #11](https://github.com/x4342/marketplace/issues/11). PostgreSQL verified order-before-quote, replay, stock contention, expiry, authorization, and all fulfillment types. Chromium completed physical and automatic digital purchases without JavaScript. Local worker and five-width layout checks passed. |
| M6 Reviews | M5 | Eligible buyer, one written 1–5 star review per order, reject unpaid/self/unrelated, derive ratings and insights from legitimate data, protect order details. | Implemented and locally verified. [PR #15](https://github.com/x4342/marketplace/pull/15) · [Issue #14](https://github.com/x4342/marketplace/issues/14). Six PostgreSQL suites passed, including eligibility and uniqueness, review privacy, rating thresholds, moderator role boundaries, appeals, and seller totals. Chromium verified review submission and public rendering with JavaScript disabled at five widths. |
| M7 Ledger | M5–M6 | Balanced append-only entries, replay-safe settlement, no concurrent overspending, payout authorization distinct from mock settlement. | Implemented and locally verified. [PR #17](https://github.com/x4342/marketplace/pull/17) · PostgreSQL passed append-only journals, upgrade backfill, concurrent reservations, payment replay, refunds, reversals, and failure recovery. Chromium passed payout through reconciliation without JavaScript across the five target widths. [Issue #16](https://github.com/x4342/marketplace/issues/16). |
| M8 Administration | M4–M7 | Reports, appeals and disputes, role-scoped queues, audit evidence, denial releases reservation as applicable, no finance-admin shipping access by default. | Implemented and tested in simulation |
| M9 Hardening | M0–M8 | Regression suite, restore and reconciliation evidence, accessibility/Tor/no-JS review, threat model and owner readiness report. Public launch and live funds require approval. | Sandbox hardening and private Tor transport verified. Tor Browser and formal accessibility sign-off pending. [Readiness report](release-readiness.md) · [Issue #20](https://github.com/x4342/marketplace/issues/20). |
| M10 Local Container Deployment | M9 | Non-root image, localhost-only Portainer stack, gated migrations, fixture opt-in, recovery instructions, and container smoke in CI. | [Issue #22](https://github.com/x4342/marketplace/issues/22) · [Arch Linux runbook](local-container-deployment.md). |
| M11 Dark-only interface and listing media | M10 | Dark-only shell with Overview preserved. Bounded square image derivatives, interactive crop and no-script fallback, historical image compatibility, and persistence. | [Dark-only PR #25](https://github.com/x4342/marketplace/pull/25) passed both CI workflows. [Media PR #26](https://github.com/x4342/marketplace/pull/26) has local Go, PostgreSQL and Chromium verification. [Issue #24](https://github.com/x4342/marketplace/issues/24). |
| M12 Profiles and authorization | M11 | Public profiles with persistent cropped avatars. Three roles, independent staff decisions, fresh financial authentication and secure local administrator bootstrap. | [Issue #27](https://github.com/x4342/marketplace/issues/27). [Authorization and bootstrap guide](authorization.md) · [Profile data and storage](public-profiles.md). Stacked on [M11 media PR #26](https://github.com/x4342/marketplace/pull/26) while it remains unmerged. |

## Requirement tracking

Identifiers refer to the V5 blueprint. The engineering owner, prerequisite, review link, and evidence for each entry are inherited from its milestone row until the feature has its own review record.

<!-- feature-checklist -->

### M0

- [x] A01 — Implemented
- [x] A02 — Implemented
- [x] A03 — Implemented
- [x] A04 — Implemented
- [x] A05 — M0 scope implemented and verified
- [x] G01 — M0 scope implemented and verified
- [x] G02 — M0 scope implemented and verified
- [x] G03 — M0 scope implemented and verified
- [x] G04 — Resolved via the owner-authorized README initialization and feature PR
- [x] G05 — M0 scope implemented and verified
- [ ] P03 — M0 scope verified. Remaining feature work follows the milestone gates.
- [x] T01 — M0 scope implemented and verified
- [x] T02 — M0 scope implemented and verified
- [x] T03 — M0 scope implemented and verified
- [x] T06 — M0 scope implemented and verified
- [x] T07 — Implemented
- [x] U01 — M0 scope implemented and verified
- [x] U02 — M0 scope implemented and verified
- [ ] U03 — M0 scope verified. Remaining feature work follows the milestone gates.
- [ ] U04 — M0 scope verified. Remaining feature work follows the milestone gates.
- [ ] U05 — M0 scope verified. Remaining feature work follows the milestone gates.
- [ ] U07 — M0 scope verified. Remaining feature work follows the milestone gates.
- [ ] Q03 — M0 scope verified. Remaining feature work follows the milestone gates.
- [ ] Q04 — M0 scope verified. Remaining feature work follows the milestone gates.
- [ ] Q05 — M0 scope verified. Remaining feature work follows the milestone gates.
- [ ] Q08 — M0 scope verified. Remaining feature work follows the milestone gates.
- [x] PM01 — Implemented
- [ ] PM02 — M0 scope verified. Remaining feature work follows the milestone gates.
- [ ] PM03 — M0 scope verified. Remaining feature work follows the milestone gates.
- [ ] PM04 — M0 scope verified. Remaining feature work follows the milestone gates.
- [ ] PM05 — M0 scope verified. Remaining feature work follows the milestone gates.
- [x] S01 — M0 scope implemented and verified
- [x] S04 — M0 scope implemented and verified

### M1

- [x] P02 — M1 scope verified
- [x] C01 — M1 scope verified
- [x] DB01 — M1 scope verified
- [x] R03 — M1 scope verified
- [ ] Q01 — Identity and role boundaries verified. Feature-specific resource checks follow in later milestones.
- [x] S02 — M1 scope verified
- [x] S03 — M1 scope verified

### M2

- [ ] P09 — Partial. Review-backed facets await real review data and seller lifecycle work remains in later milestones.
- [ ] U06 — Not started
- [ ] D01 — Partial. Review-backed facets await real review data and seller lifecycle work remains in later milestones.
- [x] D02 — Discovery slice implemented and verified in branch CI
- [ ] D03 — Partial. Review-backed facets await real review data and seller lifecycle work remains in later milestones.
- [ ] D04 — Partial. Review-backed facets await real review data and seller lifecycle work remains in later milestones.
- [x] D05 — Discovery slice implemented and verified in branch CI
- [x] D06 — Native mobile drawer, keyboard open/close, focus and always-visible sort verified without JavaScript.
- [x] D07 — Discovery slice implemented and verified in branch CI
- [ ] D08 — Partial. Review-backed facets await real review data and seller lifecycle work remains in later milestones.
- [x] D09 — Discovery slice implemented and verified in branch CI
- [x] D10 — Discovery slice implemented and verified in branch CI
- [ ] DB02 — Partial. Review-backed facets await real review data and seller lifecycle work remains in later milestones.
- [x] R01 — Discovery slice implemented and verified in branch CI
- [x] Q02 — Discovery integration and browser regression coverage for supported facets, sorting, history and reset.

### M3

- [x] P06 — Physical, automatic digital, and manual digital fulfillment verified in simulation
- [x] C02 — Seller lifecycle and uploads verified
- [ ] C03 — Seller labels, management, and vacation visibility verified. Order and analytics integrations follow.
- [x] R02 — Published details and seller forms verified

### M4

- [x] P04 — Listing contact and private verified-payment event verified
- [ ] T04 — Not started
- [x] C04 — Private messaging scope implemented and verified
- [x] C05 — One immutable private event per verified order, including when no thread existed
- [x] C06 — Private messaging scope implemented and verified
- [ ] C07 — Message, payment, shipment, manual delivery and download links verified. Dispute and payout notifications follow their owning milestones.
- [x] DB03 — Private messaging scope implemented and verified
- [x] R04 — Private messaging scope implemented and verified

### M5

- [x] P07 — Simulated direct checkout verified. Live provider and asset decisions remain open
- [ ] P10 — Not started
- [x] O01 — Committed unpaid order, stock reservation, and immutable snapshot verified before quoting
- [ ] O02 — Simulated payment, fulfillment, expiry, cancellation, and problem flag verified. Reviewed and refunded states await the later dispute and financial workflows.
- [x] O03 — Participant-scoped shipping, dispatch, and receipt verified in simulation
- [x] O04 — Versioned protected automatic download and outsider denial verified
- [x] O05 — Seller task, deadline, private delivery, and buyer problem route verified
- [ ] DB04 — Order, timeline, shipment, and fulfillment tables verified. Reviews and dispute tables follow in M6 and M8.
- [x] R05 — Orders, mock checkout, and timeline forms verified
- [x] Q06 — Commerce integration and no-JavaScript acceptance flows verified

### M6

- [x] P05 — M6 verified in simulation
- [x] O06 — M6 verified in simulation
- [x] R06 — M6 verified in simulation

### M7

- [x] P08 — USD/EUR earnings, simulated commission and approved TEST payouts verified. Live operation remains open.
- [x] T05 — Replaceable provider interfaces exercised with deterministic simulation adapters.
- [x] F01 — Simulated direct checkout, denomination separation and commission verified. Live operation remains open.
- [x] F02 — M7 simulated finance scope verified
- [x] F03 — M7 simulated finance scope verified
- [x] F04 — M7 simulated finance scope verified
- [x] F05 — M7 simulated finance scope verified
- [x] F06 — M7 simulated finance scope verified
- [x] F07 — M7 simulated finance scope verified
- [x] DB05 — M7 simulated finance scope verified
- [x] DB06 — M7 simulated finance scope verified
- [x] R07 — M7 simulated finance scope verified
- [x] Q07 — M7 simulated finance scope verified

### M8

- [x] A10 — Listing, seller and conversation reports, restrictions, delisting, independent content appeals, and audited access to chat report text.
- [x] A11 — Private case evidence and fulfillment snapshots, audited outcomes with ledger effects, and independent appeals.
- [x] A12 — Status-filtered payout review and separate holds that survive denial until released.
- [x] A13 — PostgreSQL integration checks for role boundaries, shipping privacy, and immutable audit history.
- [x] A14 — Filterable queues, reasoned confirmations, and timestamped audit records.
- [x] U08 — No-JavaScript Chromium checks for dispute intake, evidence, queue layout and moderator decisions at viewport widths from 320 to 1440 px.
- [x] O07 — Typed complaint, case history, participant notices and appeals, with atomic simulated hold, release and refund transitions.
- [x] DB07 — Migration 00009 adds and backfills moderation records and audit events. Empty-schema rollback and populated-history protection verified.
- [x] R08 — Role-scoped dispute, content, finance and audit routes.

Local verification includes lint, race-enabled Go tests, both binaries, eight PostgreSQL integration suites, fixture replay, a populated M7 upgrade, and the targeted Chromium dispute test. Live policy and release review remain in X03, X06, and M9.

### M9

- [ ] P01 — Private Tor transport verified with Chromium over a loopback-only onion network. Tor Browser and chosen release ingress remain pending.
- [ ] P11 — Deferred by V5
- [x] DB08 — Ownership, immutable history, uniqueness, journal accounting and restored data checked.
- [ ] R09 — Representative browser checks pass. Complete assistive-technology sign-off remains pending.
- [ ] S05 — Draft scope, report handling, appeals and retention decisions recorded in the [policy review](policy-review.md). Owner approval is pending.
- [ ] X01 — Open decision
- [ ] X02 — Open decision
- [ ] X03 — Open decision
- [ ] X04 — Open decision
- [ ] X05 — Open decision
- [ ] X06 — Open decision
- [ ] X07 — Open decision

M9 evidence covers Q01–Q08 within the sandbox scope described in the [readiness report](release-readiness.md). Tor Browser, assistive technology, incident ownership, and production backup and recovery controls remain open.
