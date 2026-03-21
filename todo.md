# arcline-portal — Customer Dashboard

Web UI for Arcline customers: SSL expiry tracking, one-click static deployment,
log viewer. Sits alongside or integrates with WHMCS for billing.

## Stack
- Language: Go (backend) + vanilla HTML/CSS/JS (Arcline design system)
- Storage: PostgreSQL or SQLite
- Auth: session-based (bcrypt passwords) + optional TOTP 2FA
- Deployment: single binary + embedded static assets

## Modules

### 1. SSL Expiry Dashboard
- Customer adds domains to their account
- System checks cert expiry daily (x509 via tls.Dial)
- Dashboard shows all domains with expiry date + days remaining
- Color coding: green >30d, amber 14-30d, red <14d
- Email alerts: 30d, 14d, 7d before expiry
- [ ] Domain management (add/remove/verify ownership via DNS TXT)
- [ ] Background cert checker (goroutine + ticker)
- [ ] Alert email templates
- [ ] Dashboard view

### 2. One-Click Static Deployment
- Customer connects GitLab repo (OAuth) or uploads a zip
- On push to main → webhook → Arcline pulls repo, builds (optional), rsync to docroot
- Supported: static HTML, Hugo, Jekyll, plain PHP
- Build commands configurable per site
- [ ] GitLab OAuth integration
- [ ] Webhook receiver (verify HMAC signature)
- [ ] Build runner (sandboxed subprocess, timeout)
- [ ] Deploy: rsync to customer docroot via internal SSH
- [ ] Deploy log viewer (streaming via SSE)
- [ ] Rollback: keep last 3 deployments, one-click restore

### 3. Log Viewer
- Customer views their access/error logs in browser (no SSH required)
- Filtered by: date range, status code, IP, path
- Tail mode: live stream via SSE
- [ ] Log file reader (tail -F equivalent in Go)
- [ ] Filter engine
- [ ] SSE streaming endpoint
- [ ] Frontend log table (virtual scroll for large logs)

### 4. Support Tickets
- Simple ticket system (open, in-progress, closed)
- Customer creates ticket → email notification to blake@arclineit.com
- Blake replies via email → reply appears in ticket thread
- [ ] Ticket CRUD
- [ ] Email-in (IMAP polling or inbound SMTP hook)
- [ ] Email-out (SMTP on ticket create/reply)
- [ ] Ticket list + thread view

## Auth
- [ ] Register / login / logout
- [ ] Password reset (email link, 1h expiry)
- [ ] TOTP 2FA (optional, QR code enrollment)
- [ ] Session management (secure cookie, server-side store)

## Tasks (phase 1 — MVP)
- [ ] Project scaffold (Go + embedded FS for templates/assets)
- [ ] Database schema (users, domains, deployments, tickets, sessions)
- [ ] Auth system (register, login, sessions, password reset)
- [ ] SSL dashboard (domain add/verify, cert check, expiry display)
- [ ] Basic ticket system
- [ ] Arcline design system applied to all views
- [ ] systemd unit + nginx reverse proxy config
- [ ] README: deployment guide, env vars reference
