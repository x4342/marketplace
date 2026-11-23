# Private Marketplace engineering guide

- Follow the V5 product contract and the gates in the implementation plan. Treat proposed defaults as reversible and record their rationale. Leave unresolved policy with the owner.
- Keep a Go modular monolith using net/http, templ, HTMX, Tailwind CSS, daisyUI, PostgreSQL, pgx, sqlc, and goose. Keep transport, domain rules, persistence, and providers separate. Use prepared queries, transactions for state transitions, and database constraints for invariants.
- Render whole pages on the server. Build working GET forms, POST forms, links, validation, and feedback before adding HTMX. Serve all assets locally, use system fonts, and avoid tracking.
- Use the Minimal Marketplace Dashboard design with one dark daisyUI theme. Use Discover as the home page and keep operational status inside the administrator dashboard. Keep neutral surfaces, one accent, 4px spacing increments, approximately 8px control and 12px card radii, and visible keyboard focus. Verify mobile at 320 and 375px, tablet at 768px, and desktop at 1024px and above. Use one mobile and two desktop discovery columns and always show sort above results.
- Check resource ownership and permissions server-side. Use the centralized three-role authorization model. Staff finance mutations require per-action password confirmation and independent decisions. Protect mutations against CSRF, minimize data collection, and exclude credentials, session tokens, wallet secrets, message bodies, and shipping addresses from logs.
- Use integer monetary units and transactional, idempotent, append-only accounting. Keep all payment and payout providers simulated. Live integrations and public deployment require owner approval.
- Format Go and templ source. Exercise positive, negative, migration, responsive, and no-JavaScript cases as applicable. Record actual evidence and gaps. Open focused feature PRs and leave merging and default-branch initialization to the owner unless authorized.
- Keep media validation and image derivatives in the shared media package. Apply authorization before decoding. Store only normalized new images and preserve historical media and immutable order snapshots.

- Use stable category UUIDs for administration. Preserve old slugs, listing associations, and archived category and role changes. Run upgrade and restore drills only with isolated data.

- M14 registration uses canonical lowercase ASCII alphanumeric usernames of 3–20 characters. Existing usernames and identity references are retained. Validate new passwords as 8–128 Unicode code points and preserve their supplied bytes during authentication.
- Serve the only global keyword field in the shared header. Discover defaults to products. Use indexed, paginated public username search and keep product facets out of Users mode.
