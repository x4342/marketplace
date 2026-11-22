# Engineering decisions

| Date | Decision | Reason |
| --- | --- | --- |
| 2026-11-22 | Use the system color scheme until a user selects Light or Dark using a server POST. Persist the selection in a first-party cookie. | The preference works without JavaScript and independently of authentication. |
| 2026-11-22 | Bind the development HTTP service and PostgreSQL host port to loopback. Load CSS and optional HTMX directly from local assets. | Keeps development isolated and avoids browser requests to third-party services. |
| 2026-11-22 | Use one PostgreSQL fixture to exercise the worker's heartbeat and the readiness probe's schema dependency. | Keeps M0 verifiable without introducing a job queue before a feature needs one. |
