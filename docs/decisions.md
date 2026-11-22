# Engineering decisions

| Date | Decision | Reason |
| --- | --- | --- |
| 2026-11-22 | Use the system color scheme until a user selects Light or Dark using a server POST. Persist the selection in a first-party cookie. | The preference works without JavaScript and independently of authentication. |
| 2026-11-22 | Bind the development HTTP service and PostgreSQL host port to loopback. Load CSS and optional HTMX directly from local assets. | Keeps development isolated and avoids browser requests to third-party services. |
| 2026-11-22 | Use one PostgreSQL fixture to exercise the worker's heartbeat and the readiness probe's schema dependency. | Keeps M0 verifiable without introducing a job queue before a feature needs one. |
| 2026-11-22 | M1 uses canonical lowercase usernames, Argon2id, 12-hour opaque database sessions, five one-use recovery codes, and bounded account/action and process-wide authentication limits. Recovery replaces all prior sessions and codes. | Username-only identity works without email or tracking. Token digests and hashed passwords keep plaintext credentials out of PostgreSQL. Process-wide limits are intended for the single-process local milestone and should be reviewed before a production topology. |
| 2026-11-22 | M1 uses same-origin checks and first-party form tokens. Session and form cookies are HttpOnly and SameSite Strict, with Secure enabled on TLS requests. | Forms work without JavaScript, including in the loopback HTTP development environment. Production TLS termination and retention are release-review work. |
