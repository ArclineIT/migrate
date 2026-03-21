# arcline-portal

Customer dashboard for arclineit.com. Provides SSL expiry monitoring, one-click static site deployment, a log viewer, and a basic support ticket system — without requiring customers to SSH into anything.

Sits alongside WHMCS for billing; handles everything WHMCS doesn't.

## Status

Planned. Not yet started.

## Stack

- Go backend, vanilla HTML/CSS/JS (Arcline design system)
- PostgreSQL or SQLite
- Session-based auth with optional TOTP 2FA
- Ships as a single binary with embedded static assets

## Modules

### SSL Expiry Dashboard
Customers add domains; the system checks cert expiry daily and sends alerts at 30/14/7 days. Color-coded: green > 30d, amber 14–30d, red < 14d.

### Static Deployment
Connect a GitLab repo or upload a zip. On push to main, Arcline pulls, builds, and deploys via rsync. Supports static HTML, Hugo, Jekyll, plain PHP. Last 3 deployments kept for rollback.

### Log Viewer
Browse access/error logs in the browser. Filter by date, status code, IP, path. Live tail via SSE.

### Support Tickets
Customer opens a ticket; Blake gets an email. Replies go back into the thread. No third-party helpdesk.

## Environment variables

To be documented once scaffold is started.

## Deployment

Single binary + systemd unit behind nginx. See [todo.md](todo.md) for the full task list.
