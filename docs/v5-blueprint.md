# PRIVATE MARKETPLACE V5.0

**Product + Engineering + Agent Execution Blueprint**  
**Date:** 22 November 2026  
**Audience:** ChatGPT Work, GitHub-connected coding agents, project owner  
**Source:** V4 implementation blueprint, preserving V3 requirements and incorporating final V3-to-V4 audit refinements.  
**Status:** Final implementation handoff; NOT authorization for public launch or real-money operation.

**V5 precision updates:** Always-visible mobile sorting; notification deep links; stronger listing draft/upload UX; availability choices; persistent theme/performance rules; review metadata; reset/history behavior; purchase-event thread creation; vacation-mode obligations. These refine existing requirements, not new financial approvals.

## 00. START HERE: AGENT EXECUTION CONTRACT

**MISSION.** Implement a real, maintainable, privacy-conscious marketplace for lawful physical and digital goods. Read this entire document before editing code. Execute in milestone order (Section 02), using the authorized GitHub repository. The goal is integrated, tested vertical slices - not a design prototype or a large unreviewable code drop.

**Instruction precedence:** (1) explicit owner decisions; (2) CONFIRMED V5 requirements; (3) PROPOSED V5 defaults; (4) documented repository constraints; (5) engineering judgment. When rules conflict, preserve existing work, document the conflict, and request clarification only if the decision is material.

**Status vocabulary:** CONFIRMED = owner-approved product behavior; PROPOSED = reversible design/engineering default; OPEN = decision requiring owner/policy approval; DEFERRED = not needed for first release. Never present PROPOSED/OPEN as approved fact.

### 00.1 First actions (before application code)

- **A01 / Identify repo:** Use the GitHub connection to locate the designated repository. If multiple targets are plausible, request the repository URL/name. Do not edit an arbitrary repository.
- **A02 / Audit:** Inspect README, branches, code, tests, CI, migrations, configuration, and any existing AGENTS.md. Map implemented functionality against Section 02; do not overwrite working features.
- **A03 / Plan in repo:** Create/update `docs/implementation-plan.md`, listing milestones, dependencies, acceptance checks, status, blockers, and links to PRs. Add/update `AGENTS.md` with this document's stack, design, security, and testing rules; respect pre-existing instructions.
- **A04 / Start building:** Implement the earliest incomplete, unblocked vertical slice; do not stop at architecture diagrams or a plan. Continue through milestones as tools and permitted session capacity allow.
- **A05 / Be truthful:** Report exactly what was edited, committed, tested, proposed, blocked, and not executed. Never claim completion or success without evidence.

### 00.2 GitHub workflow

- **G01:** Use feature branches and focused commits. Prefer one milestone or coherent vertical slice per pull request; keep default branch protected when possible.
- **G02:** Before a PR, run format, compile, tests, migration checks, and applicable UI/authorization checks. Include commands, outcomes, risks, and screenshot evidence when available.
- **G03:** Do not force-push, delete unrelated branches, change repository permissions, commit secrets, overwrite owner changes, or modify production resources without explicit authorization.
- **G04:** If GitHub/plugin actions cannot commit or open PRs, finish locally to the extent supported, provide a change summary/patch or branch artifacts, and state the exact blocker.
- **G05:** Keep documentation synchronized with the real code. Record reversible assumptions in `docs/decisions.md` and unresolved approvals in `docs/open-decisions.md`.

### 00.3 Required delivery loop

- For each slice: **inspect -> design minimal contract -> migration -> backend -> templ UI -> tests -> manual responsive check -> docs -> PR -> report**.
- Prefer real database-backed flows with seeded development data. Do not label static examples, dead buttons, or hardcoded status changes as implemented features.
- If verification fails, repair or document it honestly before continuing. A passing frontend demo does not excuse missing server authorization or broken accounting.
- Default to the simplest design consistent with V5. Avoid speculative infrastructure and broad rewrites.

### 00.4 Stop/approval rules

- **HARD STOP:** Do not connect live wallets, accept real crypto, broadcast withdrawals, or deploy a public real-money marketplace before Section 16's legal, security, provider, and policy gates receive explicit owner approval. Build mocks, interfaces, and tests meanwhile.
- **ASK OWNER:** Choosing assets/networks, custody/payment provider, countries/allowed categories, dispute or release periods, FX/fee policies, withdrawal limits, production domain, destructive repo actions, or changing confirmed behavior.
- **DO NOT BLOCK:** Minor styles, component props, naming, reversible internal structure, mock test data, or routine coding choices. Document important assumptions and proceed.

### 00.5 Milestone report format

`Milestone | Status (done/partial/blocked) | PR/commit | Files/modules | Tests run + results | Manual UI checks | Known gaps | Next step | Owner decisions needed`.

## 01. PRODUCT CONTRACT: IMMUTABLE BASELINE

- **P01 [CONFIRMED]:** Tor-accessible marketplace for lawful physical and digital goods; any registered account can buy and sell.
- **P02 [CONFIRMED]:** Username + password authentication; no mandatory email or phone. Recovery codes are the recommended recovery mechanism.
- **P03 [CONFIRMED]:** Modern mobile-first dashboard via Go + templ + HTMX + Tailwind + daisyUI. Desktop discovery has a separate filter rail and **two listing-card columns**.
- **P04 [CONFIRMED]:** Any registered user can message a published listing owner without buying. A verified paid order produces the private, immutable `Bought "Product Name"` conversation system event.
- **P05 [CONFIRMED]:** A buyer can leave one 1-5 star written review only for an eligible purchased order.
- **P06 [CONFIRMED]:** Physical fulfillment plus digital automatic protected download AND manual seller delivery.
- **P07 [CONFIRMED]:** Buyer checkout is crypto-only directly from the buyer's wallet; no prefunded buyer balance is required.
- **P08 [CONFIRMED]:** Sellers see USD/EUR-denominated earnings; the platform charges a 5% transaction commission; seller payouts are cryptocurrency, initially requiring admin approval.
- **P09 [CONFIRMED]:** Seller storefronts, favorites, in-app notifications, basic seller analytics, vacation mode, order timelines, disputes, and report/appeal moderation.
- **P10 [PROPOSED]:** Single seller per order; 5% commission calculated on item subtotal, excluding shipping/taxes/provider fees until Section 16 terms are approved.
- **P11 [DEFERRED]:** Buyer stored balances, auctions, multi-seller carts, subscriptions, coupons, native apps, general chat attachments, cross-chain automation, advanced personalization, and extensive product variants.

## 02. EXECUTION ORDER AND ACCEPTANCE GATES

**Rule:** Run phases in dependency order. Each phase needs a usable user journey, database persistence, negative tests, and responsive/no-JS checks. Financial activity stays simulated until separately approved. Create GitHub issues/tasks for phase slices; mark complete only after its gate passes.

### M0 - Foundation and design shell

**Build:** Go module, `net/http`, templ rendering, HTMX, Tailwind/daisyUI, PostgreSQL/pgx/sqlc/goose, configuration template, CI, server-side error handling, logging policy, shared responsive app shell, theme tokens, basic worker scaffold, dev fixtures. Set up `.gitignore`, secrets documentation, health checks, local commands.

**Gate:** Fresh checkout can configure, migrate, compile, run, format, and test. Light/dark shell works at 320, 375, 768, 1024 and desktop widths; theme preference persists. No third-party asset dependency in rendered pages.

### M1 - Identity, roles, and secure sessions

**Build:** Register/login/logout, password hashing, recovery codes, server-side sessions, role assignments, CSRF, resource authorization helpers, audit/security events, responsive forms. Separate content moderator and finance administrator permissions at this foundation stage.

**Gate:** No email required; cross-user object access denied; invalid credentials/recovery attempts handled; role-gated routes work; session expiry and CSRF regression tests pass.

### M2 - Catalog discovery and storefront basics

**Build:** Categories, physical/digital listing model and read-only detail pages; seeded items; database-backed keyword search, listed filters and sorting, URL state, favorites, seller storefronts, responsive card grid, mobile filter drawer, pagination.

**Gate:** Desktop uses two listing-card columns, mobile one; sort is visible above results on every device; GET/no-JS search and filter work; `In stock` and `Available to order` behave distinctly; invalid facets fail safely; URL round-trip, Reset, pagination and browser history preserve intended state; no fake seller ratings.

### M3 - Seller listing lifecycle

**Build:** Create/edit/preview/draft/publish/pause/remove permitted listings, public image handling, physical shipping/condition metadata, digital auto/manual delivery metadata, stock/availability, vacation mode, seller management screens. Support Save Draft at every creation stage, upload progress/rejection feedback, and desktop/mobile previews.

**Gate:** Mobile listing form preserves data across errors; Save Draft works from every stage; upload errors are understandable; ownership enforced; file validation tested; paused or vacationing seller listings cannot be purchased; existing orders and messages remain actionable; prior order snapshots are never rewritten by seller edits.

### M4 - Open messaging and notification foundation

**Build:** Listing-linked conversations between any logged-in user and owner, text messages, inbox, unread counts, actionable in-app notices, report/block, modest HTMX polling, paginated history, mobile full-screen thread. Define purchase-system-event schema/renderer now; event creation is wired to verified order in M5. Notifications must deep-link to the correct message, order, delivery, dispute, seller task or payout.

**Gate:** Nonbuyers can message; unrelated users cannot view threads; sending works without JS; block/report works; no user can forge a purchase event via normal message API.

### M5 - Orders and mock fulfillment

**Build:** Order created *before* payment quote with immutable item snapshot; mock checkout and payment verification events; order timeline; physical shipment workflow; digital protected auto-delivery and manual delivery tasks; exception states (expiry, cancellation, failed fulfillment). Wire verified payment to one private purchase system event.

**Gate:** Seeded buyer completes two mock end-to-end purchases (physical and digital); verified payment creates or locates the correct private conversation and adds exactly one purchase event, even when no earlier chat exists; duplicate events do not double-create paid state; access-controlled download denies other accounts.

### M6 - Reviews, trust, and seller insights

**Build:** Verified-order 1-5 written reviews, eligibility and uniqueness enforcement, report/appeal moderation, seller rating summary with sample-size guard, review count/date/distribution/product context displays, favorites polish, seller view/order/revenue aggregates, notification links.

**Gate:** Unpaid, ineligible, unrelated, and self-reviews denied; one review per qualifying order; ratings are derived from legitimate review data; no sensitive order details exposed publicly.

### M7 - Financial ledger and simulated payout lifecycle

**Build:** Immutable double-entry ledger; pending/available/held/reserved buckets; 5% fee calculation under PROPOSED base; quote/payment/payout provider interfaces; deterministic mocks; idempotent payment settlement; payout reservation and refusal/approval/reconciliation states; separate currency and crypto units.

**Gate:** Balances derive from balanced ledger; no double-credit on replay; concurrent withdrawals cannot overspend; payout approval is distinct from settlement; all simulations verified by integration tests. **Do not enable real wallets.**

### M8 - Administration, dispute center, and audit completion

**Build:** Content reports, chat/listing/user actions, order disputes, evidence, appeals, admin payout approval/denial queue, financial holds, role-specific UIs, append-only admin audit events, mobile-friendly admin records. Some report/block scaffolding begins in M1/M4; complete workflows here.

**Gate:** Moderators cannot approve payouts; finance admins cannot edit ledger lines or see shipping details by default; denial releases reservation unless a separate hold exists; every privileged action is auditable.

### M9 - Hardening and release-readiness review

**Build:** Automated/visual regression pass, threat model, secure upload checks, accessible UI QA, Tor/no-JS and slow-network checks, backup/restore rehearsal, reconciliation and refund simulation, incident procedures, retention rules, policy documents, production configuration review.

**Gate:** No critical auth/payment defects; tests and restore evidence documented; unresolved Section 16 decisions prevent live launch; owner receives a readiness report rather than an unsupported "production-ready" claim.

## 03. STACK AND ARCHITECTURAL CONTRACT

- **T01:** Go `net/http` modular monolith: `cmd/web`, `cmd/worker`, `internal/http`, `internal/domain`, `internal/services`, `internal/db`, `internal/integrations`, `web/templates`, `web/static`, `db/migrations`, `db/queries` (adapt to existing repo if sound).
- **T02:** templ renders complete HTML pages and HTMX fragments. Core actions work with standard GET/POST routes; no JS is required to search, filter, submit forms, send messages, or read order status.
- **T03:** PostgreSQL with pgx, sqlc, goose; use SQL transactions for money/stock/order state transitions. Native Postgres full-text search and indexes before external search services.
- **T04:** Go worker plus PostgreSQL-backed durable jobs for notifications, timeouts, delivery cleanup, and payment reconciliation; retries must be safe and observable.
- **T05:** Replaceable `PaymentProvider`, `QuoteProvider`, `PayoutProvider` boundaries, implemented with deterministic mocks first; production adapters are a distinct approved project.
- **T06:** Locally hosted CSS, HTMX, icons, fonts (prefer system fonts), and validated images. Serve appropriately sized/compressed listing images, use safe caching and progressive disclosure for slow Tor connections. Private digital goods remain inaccessible from public static routes; never publicly cache protected files.
- **T07:** Do NOT add React, Next.js, microservices, WebSockets, Redis, Kubernetes, a search cluster, or external analytics without a measured need and owner sign-off.

## 04. DESIGN SYSTEM AND RESPONSIVE CONTRACT

- **U01 [PROPOSED]:** Philosophy = *Minimal Marketplace Dashboard*: calm neutral surfaces, restrained single accent, clear visual hierarchy, subtle borders, low animation, compact but readable content. Product browsing feels inviting; moderation/finance views prioritize precision.
- **U02 [PROPOSED]:** Design tokens: 4px spacing base (4/8/12/16/24/32), ~8px control radius and ~12px card radius, readable 14-16px body, three button/input sizes, one icon set, light/dark with accessible contrast. Persist the selected theme without third-party services.
- **U03 [REQUIRED]:** Build reusable templ/daisyUI primitives: `AppShell`, `Topbar`, `Sidebar`, `MobileDock`, `PageHeader`, `ListingCard`, `PriceLabel`, `FilterRail`, `FilterDrawer`, `SortSelect`, `ActiveChips`, `SellerRating`, `OrderTimeline`, `CheckoutStatus`, `ConversationThread`, `PurchaseSystemEvent`, `BalanceSummary`, `AdminQueue`, `EmptyState`, `InlineAlert`, `LoadingSkeleton`, `ConfirmDialog`.
- **U04 [REQUIRED]:** Every interactive component defines input/error/loading/disabled/success states, keyboard/focus behavior, authorization behavior, and mobile behavior. Show actionable feedback for empty results, expired quotes, failed uploads, and unavailable products.
- **U05 [PROPOSED]:** Mobile <768px: compact header, bottom dock `Discover | Sell | Orders | Messages | Account`, one listing column, full-height filter drawer, full-screen chat, safe-area padding.
- **U06 [PROPOSED]:** Tablet 768-1023px: collapsible navigation, two cards where legible, collapsible filter panel/drawer. Desktop >=1024px: persistent app nav + dedicated filter rail + two listing-card columns within bounded width.
- **U07 [REQUIRED]:** No unintended horizontal scrolling at 320-375px; touch targets aim for 44x44px; visible focus, contrast, semantic labels, reduced motion and keyboard operation aligned to WCAG 2.2 AA.
- **U08 [REQUIRED]:** Desktop admin tables become stacked cards/expandable records on mobile. Sticky actions cannot obscure content, modals or the software keyboard.

## 05. DISCOVERY SPECIFICATION: DATA, FILTERS, SORT

**D01 / Cards:** Consistent image ratio and placeholder, title, price/currency, type/category, seller summary, optional meaningful rating/count, availability/delivery status, favorite action. Listing detail adds gallery, purchase button, seller contact, shipping/delivery terms, verified reviews, report link. Reviews display verified count, dates, rating distribution and relevant product context without disclosing private orders.

**D02 / Essential facets:** hierarchical category/subcategory; `type=all|physical|digital`; numeric min/max price with selected display currency; availability supports distinct `In stock` and `Available to order` choices where relevant. Never show paused/unpublished listings as purchasable.

**D03 / High-priority facets:** shipping destination and condition (`new|used|refurbished`) for physical only; `delivery=automatic|manual` for digital only; seller-rating threshold. Medium: verified-review presence and seller/storefront selection. Category-specific attributes are DEFERRED.

**D04 / Conditional rules:** Switching type/category clears incompatible facets. The server validates each option against allowlists, does not trust client-provided facet IDs, and reports cleared/invalid choices where relevant.

**D05 / Desktop:** Separate filter rail, results count, visible sort selector, applied filter chips and reset. GET forms with optional HTMX enhancement; do not build a JS-only filter state.

**D06 / Mobile:** Full-height drawer with staged choices, active count, explicit `Apply filters` and `Reset all`, accessible close button; controls remain reachable on narrow screens. Keep the sort selector visible above results outside the drawer at mobile/tablet/desktop widths.

**D07 / URL:** Canonical GET state: `/discover?q=keyboard&type=physical&category=accessories&min=20&max=120&currency=EUR&sort=price_asc&page=2` (example; names PROPOSED). Preserve query state across pagination/back/forward, including reasonable scroll/focus restoration. Removable chips modify the canonical URL. Proposed Reset all behavior: clear facets and sort, return to page 1, retain keyword search; offer a separate Clear search action. Invalid numeric ranges show inline errors, not silent corruption.

**D08 / Sort:** `newest` (default browse, `published_at DESC,id`); `relevance` (only when searching; PostgreSQL text ranking + deterministic tie-break); `price_asc` and `price_desc` (comparable normalized price, stable tie-break). `rating` requires eligible minimum review count; `popular` requires genuine completed purchases. Hide the latter when insufficient data.

**D09 / Currency:** Catalog comparison of USD/EUR listings needs a dated documented conversion rate and consistent rounding; approximation is labeled. Browser price normalization is NOT the crypto checkout quote. FX source and quote validity remain OPEN for live use.

**D10 / Empty/errors:** Zero results show clear state and reset link; disallowed sort, invalid page, malformed price range, and obsolete facet combinations have predictable, tested responses.

## 06. ACCOUNTS, CATALOG, SELLERS, MESSAGES

- **C01 / Identity:** Unique username, secure password hash (Argon2id proposed), server-side sessions, one-time hashed recovery codes, logout/session invalidation, CSRF on mutations. Rate-limit without relying solely on Tor exit IP.
- **C02 / Listings:** `draft -> optional review -> published -> paused/removed`. Required: title, description, category, type, price + currency, quantity/availability, images, seller, status, timestamps. Physical: shipping regions, condition, handling policy; digital: delivery mode and terms. Allow Save Draft at every step, preserve form data on errors, show upload progress and specific rejection reasons, and offer desktop/mobile previews. Validate uploads and strip irrelevant metadata.
- **C03 / Seller:** Storefront shows username, optional bio/policies, published catalog, verified ratings/counts, member-since, vacation indicator. Seller dashboard shows listings, orders, messages, earnings and aggregate analytics without cross-site tracking. Vacation mode prevents NEW checkout but never suspends fulfillment of existing orders, messaging, or dispute response.
- **C04 / Conversations:** Any logged-in user may contact published listing owner; one thread per buyer/seller/listing context (or deterministic equivalent), private to participants and audited privileged access. Text-only, paginated, HTMX polling; no general attachments initially.
- **C05 / Purchase event:** On verified paid order, locate or create the buyer/seller/listing conversation if none exists; emit exactly one system event tied to real `order_id` and immutable title snapshot: `Bought "Product Name"`. It is not a user-editable message and is visible only to that conversation's participants.
- **C06 / Abuse:** In-app report/block and unread/notices. Messaging must remain usable with standard form POST and without JavaScript.
- **C07 / Notification destinations:** New message -> conversation; payment verified -> order timeline; digital goods ready -> private entitlement; seller action -> fulfillment task; dispute update -> case; payout decision -> payout detail. Notifications are in-app only and must target an authorized view.

## 07. ORDERS, FULFILLMENT, REVIEWS

- **O01 / Create first:** Persist an unpaid order, buyer, seller, item snapshot, total, currency, delivery terms and stock-reservation policy BEFORE requesting any quote. Checkout assumes one seller/order initially.
- **O02 / State:** `awaiting_payment -> payment_detected -> paid -> fulfillment_pending -> fulfilled -> completed` with constrained `expired/cancelled/disputed/under_review/refunded` exceptions. Actor, timestamp and reason accompany transitions; state changes are transactional and idempotent.
- **O03 / Physical:** Minimize and protect delivery-address fields; seller can dispatch and optionally attach carrier/tracking per policy. Buyer sees processing/shipped/delivered/problem states. Exact confirmation, return, dispute and release windows are OPEN.
- **O04 / Digital automatic:** After verified payment create a buyer-owned, access-checked digital entitlement for private content. Expiry/revocation and permitted formats need policy decisions; unauthorized downloads must fail.
- **O05 / Digital manual:** After verified payment create a seller delivery task with visible deadline, status/evidence and buyer problem route. Do not treat ordinary chat attachments as delivery.
- **O06 / Reviews:** Only buyer on one qualifying completed/eligible order can submit one 1-5 rating plus written text; no self-review. Display verified-purchase status, review count, review date, distribution and relevant product context without private order details. Report/review moderation and refund annotations preserve history.
- **O07 / Disputes:** Case keyed to order with typed complaint, participant responses, restricted evidence, timestamps, moderator decision, outcome and appeal. Release/hold/refund results must post matching ledger changes, never mutate history.

## 08. CRYPTO, ACCOUNTING, PAYOUTS (SIMULATION FIRST)

- **F01 [CONFIRMED]:** Direct external-wallet cryptocurrency payment, no buyer platform wallet. Prices and seller obligations are denominated in USD/EUR; a 5% platform commission applies to eligible sales.
- **F02 [PROPOSED]:** Commission base = item subtotal only. Shipping/taxes, processor/network charges, quote rounding, FX movement and refund allocation must be signed off before live operation.
- **F03:** Quote UI shows accounting price, asset/network, quoted crypto units, rate/time limit and fees. Payment observations are append-only and uniquely deduplicated; verified payment must never post twice.
- **F04:** Double-entry `ledger_accounts`, `journal_entries`, `journal_lines`; posted entries immutable. Monetary amounts use integer cents for USD/EUR and integers in the asset's smallest supported units for crypto; conversion is a separate journal event. Balance is DERIVED, not freely edited.
- **F05:** Seller display buckets = pending, available, held/disputed, reserved for payout and withdrawn history. Pending->available depends on OPEN fulfillment/dispute policy. Reserve withdrawals atomically and never permit overspend or double reservation.
- **F06 [CONFIRMED]:** Initial withdrawals require finance-admin approval. Proposed states: `requested -> reserved -> under_review -> approved|denied -> processing -> confirmed -> reconciled` with explicit failed/retry branches. Approval does not prove blockchain settlement; denial releases reservation unless separate documented hold.
- **F07:** Test under/overpayment, wrong chain/asset, expired quote, duplicated provider notice, chain reversal, refund, stalled/failing payout and replay. Implement provider contracts against mocks; hold real integration behind explicit approval gate.

## 09. MODERATION AND FINANCE-ADMIN BOUNDARIES

- **A10 / Content queues:** Listing/user/chat reports, abuse restrictions, delisting, appeals and immutable action reasons. Policy-defined moderator access to private conversations is minimal and audited.
- **A11 / Disputes:** Review order snapshots, fulfillment evidence and participant submissions, with allowed outcomes and auditable ledger effects; final dispute windows remain OPEN.
- **A12 / Finance queues:** Payout requests show available balance, holds, reserved amount, verification status, review action and reconciliation history. Approve/deny is separate from final settlement.
- **A13 / Permissions:** Content moderators cannot approve payouts. Finance administrators cannot edit posted journal lines or see sensitive shipping fields by default. Owner cannot retroactively edit posted ledger entries (only compensating entries).
- **A14 / Admin UX:** Dense filterable table on desktop; accessible stacked case cards on mobile; impactful actions require clear confirmation, reason, actor, timestamp and audit log.

## 10. POSTGRESQL DATA CONTRACTS

- **DB01 / Identity:** `users`, `sessions`, `recovery_codes`, `role_assignments`, `security_events`.
- **DB02 / Catalog:** `categories`, `listings`, `listing_images`, `inventory_events`, `seller_storefronts`, `favorites`, `seller_vacations` (+ optional future variants).
- **DB03 / Communication:** `conversations`, `messages`, `conversation_reads`, `conversation_system_events`, `notifications`, `reports`.
- **DB04 / Commerce:** `orders`, `order_items`, `order_status_events`, `shipping_details`, `digital_entitlements`, `manual_deliveries`, `reviews`, `disputes`, `dispute_events`, `case_evidence`.
- **DB05 / Payment:** `payment_intents`, `payment_observations`, `quotes`, `exchange_rates`, provider event keys.
- **DB06 / Finance:** `ledger_accounts`, `journal_entries`, `journal_lines`, `holds`, `seller_payouts`, `payout_reviews`, `reconciliation_items`.
- **DB07 / Administration:** `moderation_actions`, `user_restrictions`, `admin_audit_events`, `appeals`.
- **DB08 / Constraints:** UUID/nonsequential IDs; row ownership checked server-side; order snapshots immutable; unique eligible review per order; unique verified purchase event per order/thread; unique provider event key; balanced journal per entry/currency; foreign keys and indexes on published/type/category/search/price/date; restricted shipping/private-file access.

## 11. ROUTE AND COMPONENT DELIVERY CONTRACT

- **R01:** `GET /discover` -> search, validated facets, sort, chips, pagination, filter rail/drawer and result cards.
- **R02:** `GET /listings/{id}` -> detail/gallery/terms/reviews/`Message seller`/`Buy with crypto`; write routes for seller create/edit/pause/preview.
- **R03:** `GET/POST /register`, `/login`, `/logout` -> no-email flows and recovery enrollment.
- **R04:** `GET /messages`, `GET /messages/{id}`, authenticated POST send/report/block -> private conversation and unread state.
- **R05:** `POST /orders`, `GET /orders/{id}` -> order-before-quote, mock checkout/status and delivery timeline.
- **R06:** `POST /orders/{id}/reviews` -> verified-order review gate; authenticated favorites and notifications routes.
- **R07:** `GET /earnings`, `POST /payouts`, `GET /payouts/{id}` -> ledger summaries and payout review status.
- **R08:** `GET /admin/{queue}` and role-guarded actions -> moderation, dispute and payout queues. Route names are PROPOSED contracts, not a mandate to break existing conventions.
- **R09 / Screen DoD:** Every feature screen specifies mobile/tablet/desktop layout; permissions; no-JS fallback; focus/keyboard semantics; empty/loading/error/success states; automated and manual checks.

## 12. TEST MATRIX AND RELIABILITY BUDGET

- **Q01 / Identity:** user A cannot read B's chat, order, file or shipping address; seller cannot edit another seller's listings; CSRF and sessions verified; moderator/finance role separation.
- **Q02 / Discovery:** valid facets and URL round-trip, `In stock` vs `Available to order`, incompatible filters clear safely, mobile sort always visible, reset retains search under the proposed policy, browser focus/history, price/sort tie-breaks stable, pagination preserves state, no misleading ratings/popularity, invalid inputs rejected.
- **Q03 / Mobile:** 320/375px no clipping/overflow; 768px tablet; >=1024px desktop has two card columns; mobile drawer Apply/Reset/chips work; admin cards and conversation composer usable.
- **Q04 / Accessibility:** Keyboard-only, visible focus, screenreader labels/status, target sizes, contrast, no reliance on hover, reduced motion and WCAG 2.2 AA target.
- **Q05 / No-JS/Tor:** Browse/search/filter, registration/forms, sending messages and reading order status work without JS; locally served optimized images/assets, safe caching, and acceptable high-latency behavior.
- **Q06 / Commerce:** order snapshots survive listing edits; expired quote does not show paid; failed download has recovery state; nonbuyers cannot create eligible reviews; paid order creates exactly one purchase event even when the buyer had no prior message thread; vacation mode does not block existing order actions.
- **Q07 / Ledger:** Replay payment and payout events, simulate concurrency, partial payments, refunds, denial and payout failures; invariants and available funds never break.
- **Q08 / Operations:** Migrations repeat cleanly; CI works; backup restoration exercised; security and incident documentation complete before proposing launch.

## 13. GITHUB PROJECT MANAGEMENT AND DEFINITION OF DONE

- **PM01:** `docs/implementation-plan.md`: one checklist entry per feature ID and M-phase with assigned owner/agent, status, dependency, test evidence and PR link. `docs/decisions.md`: dated architectural choices and reasons. `docs/open-decisions.md`: owner approvals required.
- **PM02:** One feature branch and reviewable PR per coherent slice. Summary includes scope, screenshots for UI changes if possible, migration behavior, tests, limitations and next dependencies.
- **PM03 / DoD:** Backend and templ UI truly integrated; schema migrated; input validated; server authorization enforced; user feedback states; mobile and no-JS experience; tests executed and passing; README/docs updated; PR/commit traceable.
- **PM04 / Evidence:** Report exact shell commands and outcomes. If tests or screenshots were not run, state `not run`; never fabricate credentials, repo links, test results or deployed URLs.
- **PM05 / Scope:** Do not build entire pages as static mockups first. Do not issue a single giant PR for the whole app. Optimize for an inspectable, working vertical slice at every checkpoint.

## 14. SECURITY, PRIVACY AND CONTENT POLICY

- **S01:** Tor access does not guarantee anonymity. Minimize collection; never log passwords, wallet secrets, message bodies or shipping addresses in ordinary diagnostics.
- **S02:** Secure/HttpOnly/SameSite session cookies, session rotation/expiry, CSRF defense, Argon2id or equivalent strong password hashing, input escaping, prepared queries, bounded and validated upload types/sizes, protected files.
- **S03:** Server authorization on every order, conversation, listing write, entitlement, review and payout; permission changes and high-impact admin actions audited. Backups encrypted, restorable and access-controlled.
- **S04:** Self-host CSS/JS/icons/images and avoid CDNs, unnecessary external requests and tracking. Ensure essential UI works without JavaScript. Define retention/deletion schedules for private content and financial records.
- **S05:** Published marketplace rules must define permitted/prohibited lawful goods, abuse reporting, takedown/appeals, relevant jurisdiction and dispute support. Do not implement facilitation of unlawful listings.

## 15. RELEASE-READY CHECKLIST (NO AUTOMATIC LAUNCH)

- Architecture reviewed, CI green, migration and rollback plan documented, restore drill passed.
- Functional buyer/seller/moderator/finance journeys demonstrated with mock payments; security and negative tests passed.
- Mobile, tablet, desktop, keyboard, reduced motion and Tor/no-JS checks documented.
- Financial ledger and reconciliation tested, refund and dispute simulations covered, privileged payout approval audited.
- Owner approves Section 16 OPEN policies, payment/provider legality, jurisdiction/content policy and all real-money integrations.
- Until then, deliver **feature-complete in simulation** where justified; never claim live-payment production readiness.

## 16. OPEN DECISION REGISTER (DO NOT INVENT ANSWERS)

- **X01 [CRITICAL]:** Legal entity, countries served, allowed goods/categories, consumer protection, sanctions/tax obligations, applicable custody/payment rules and approved provider.
- **X02 [CRITICAL]:** Supported crypto assets/networks, confirmation/finality rules, wallet/provider security, real payout routes and provider event verification.
- **X03 [HIGH]:** Separate physical/digital delivery evidence, dispute and refund windows, return process, earnings release triggers, appeals, seller protection.
- **X04 [HIGH]:** Currency selection for seller accounts, FX source, quote duration, conversion and rounding, allocation of price movement and network/processor costs.
- **X05 [HIGH]:** Exact 5% fee base (item subtotal PROPOSED), treatment of shipping/tax, minimum/maximum payout and withdrawal fees.
- **X06 [MEDIUM]:** Shipping-address retention, digital file types/size/malware checks, entitlement expiry, private storage/access retention.
- **X07 [MEDIUM]:** Onion-only or also regular web; final brand name, selected theme tokens, accessibility sign-off owner.

## 17. FINAL INSTRUCTION TO IMPLEMENTING AGENT

Read Sections 00-02 first. Identify the correct GitHub repo; audit existing code. Preserve confirmed requirements and all open decision gates. Build the earliest incomplete M-phase using the prescribed stack and design system. Verify a real vertical slice, document evidence, submit a focused PR, and continue methodically as permitted. Never enable real crypto or claim production-readiness without explicit approval. Report exact progress, blockers, and the next unblocked task.

## M14 owner-approved supersessions

M14 updates C01 and M1 registration. New usernames use 3–20 ASCII letters or digits, with case-insensitive lookup and lowercase persistence. Existing account names, including underscores, stay usable. New passwords use 8–128 Unicode code points with the exact submitted bytes. Existing credentials, recovery, hashing and session rules continue to apply.

M14 updates D07 and R01. The shared header is the single keyword search location, with Products and Users modes. Discover opens in Products mode and product facets retain the current keyword.

M14 expands P04, C04, R04 and M4. Listing conversations and verified purchase events remain. Separately, eligible members can open one direct thread per unordered pair without a listing. The responsive Messages workspace shows the latest conversation by default and honors thread and notification URLs.
