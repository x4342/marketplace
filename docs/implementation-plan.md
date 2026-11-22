# Private Marketplace V5 implementation plan

The implementation agent owns engineering and verification. The repository owner owns the approvals in [open-decisions.md](open-decisions.md). Work is ordered by milestone. Completion requires the feature gate, actual evidence, and a traceable review branch or pull request.

| Milestone | Depends on | Acceptance gate | Status and evidence |
| --- | --- | --- | --- |
| M0 Foundation | Empty repo audited | Fresh configure, migrate up/down, generate, build, format, test, and run web/worker. Local assets, persistent theme, keyboard and no-JS shell across 320/375/768/1024/1440px. | Implemented and tested. [Green CI](https://github.com/x4342/marketplace/actions/runs/45059542225) covers format, vet, race tests, builds, migration up/down/up, fixture repeat, and Chromium light/dark, no-JS, keyboard and HTMX checks at the five target widths. Branch `feature/m0-foundation`. [Issue #1](https://github.com/x4342/marketplace/issues/1) · [PR #2](https://github.com/x4342/marketplace/pull/2). |
| M1 Identity | M0 | Username-only registration and login, one-use recovery, server sessions, owner checks, CSRF, separate moderator and finance permissions, negative and expiry tests. | Implemented and tested. [Issue #3](https://github.com/x4342/marketplace/issues/3) · [PR #4](https://github.com/x4342/marketplace/pull/4) · [Green CI](https://github.com/x4342/marketplace/actions/runs/45060187899). |
| M2 Catalog | M1 | Search/filter/storefront/favorite DB flows, mobile one and desktop two columns, visible sort, distinct availability, canonical URL/reset/history, stable paging, no fabricated ratings. | Discovery gate passed. [PR #6](https://github.com/x4342/marketplace/pull/6) · [Green CI](https://github.com/x4342/marketplace/actions/runs/45062779838) verifies PostgreSQL search, permissions, card favorites, no-JS keyboard drawer, and responsive discovery. Seller-uploaded media is tracked in M3. Review-backed facets await M6 data. |
| M3 Seller lifecycle | M2 | Save Draft at each stage, feedback and validation for upload, preview and publishing, listing ownership, vacation blocks new checkout and preserves existing obligations and snapshots. | In progress. Local PostgreSQL and race tests passed for drafts, ownership, image privacy and validation, publish/edit/pause/resume/remove, and vacation visibility. Browser CI pending. [Issue #7](https://github.com/x4342/marketplace/issues/7). |
| M4 Messaging | M1–M3 | Nonbuyers can message, participants only, block/report, unread and notification links, no-JS send, purchase events cannot be forged. | Not started |
| M5 Orders | M3–M4 | Immutable snapshot before mock quote, physical and digital fulfillment, protected download, exactly one private purchase event including cases without prior chats. | Not started |
| M6 Reviews | M5 | Eligible buyer, one written 1–5 star review per order, reject unpaid/self/unrelated, derive ratings and insights from legitimate data, protect order details. | Not started |
| M7 Ledger | M5–M6 | Balanced append-only entries, replay-safe settlement, no concurrent overspending, payout authorization distinct from mock settlement. | Not started |
| M8 Administration | M4–M7 | Reports, appeals and disputes, role-scoped queues, audit evidence, denial releases reservation as applicable, no finance-admin shipping access by default. | Not started |
| M9 Hardening | M0–M8 | Regression suite, restore and reconciliation evidence, accessibility/Tor/no-JS review, threat model and owner readiness report. Public launch and live funds require approval. | Not started |

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
- [ ] Q02 — Not started

### M3

- [ ] P06 — Not started
- [ ] C02 — Seller lifecycle slice implemented. Full milestone gate remains open.
- [ ] C03 — Seller lifecycle slice implemented. Full milestone gate remains open.
- [ ] R02 — Seller lifecycle slice implemented. Full milestone gate remains open.

### M4

- [ ] P04 — Not started
- [ ] T04 — Not started
- [ ] C04 — Not started
- [ ] C05 — Not started
- [ ] C06 — Not started
- [ ] C07 — Not started
- [ ] DB03 — Not started
- [ ] R04 — Not started

### M5

- [ ] P07 — Not started
- [ ] P10 — Not started
- [ ] O01 — Not started
- [ ] O02 — Not started
- [ ] O03 — Not started
- [ ] O04 — Not started
- [ ] O05 — Not started
- [ ] DB04 — Not started
- [ ] R05 — Not started
- [ ] Q06 — Not started

### M6

- [ ] P05 — Not started
- [ ] O06 — Not started
- [ ] R06 — Not started

### M7

- [ ] P08 — Not started
- [ ] T05 — Not started
- [ ] F01 — Not started
- [ ] F02 — Not started
- [ ] F03 — Not started
- [ ] F04 — Not started
- [ ] F05 — Not started
- [ ] F06 — Not started
- [ ] F07 — Not started
- [ ] DB05 — Not started
- [ ] DB06 — Not started
- [ ] R07 — Not started
- [ ] Q07 — Not started

### M8

- [ ] A10 — Not started
- [ ] A11 — Not started
- [ ] A12 — Not started
- [ ] A13 — Not started
- [ ] A14 — Not started
- [ ] U08 — Not started
- [ ] O07 — Not started
- [ ] DB07 — Not started
- [ ] R08 — Not started

### M9

- [ ] P01 — Not started
- [ ] P11 — Deferred by V5
- [ ] DB08 — Not started
- [ ] R09 — Not started
- [ ] S05 — Not started
- [ ] X01 — Open decision
- [ ] X02 — Open decision
- [ ] X03 — Open decision
- [ ] X04 — Open decision
- [ ] X05 — Open decision
- [ ] X06 — Open decision
- [ ] X07 — Open decision
