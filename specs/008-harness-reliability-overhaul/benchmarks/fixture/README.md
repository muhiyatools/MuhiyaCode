# OrderDesk

OrderDesk is a tiny order-management backend. It is organised as four
independent modules that share no code and can each be reviewed in isolation:

- `auth/` — operator sessions, API tokens, and password policy.
- `billing/` — invoices and sales-tax computation.
- `reports/` — order summaries and CSV export.
- `notify/` — customer email and SMS notifications.

Each module is self-contained: no module imports another, and every module
only uses the standard library.
