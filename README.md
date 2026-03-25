# arcline-portal

Customer dashboard for arclineit.com. Provides SSL expiry monitoring and a support ticket system — without requiring customers to SSH into anything.

Sits alongside WHMCS for billing; handles everything WHMCS doesn't.

## Stack

- Go backend, vanilla HTML/CSS/JS (Arcline design system)
- SQLite (single file, no server required)
- Session-based auth (bcrypt + secure cookies)
- Ships as a single binary with embedded static assets and templates

## Modules

### SSL Expiry Dashboard
Customers add domains; the system checks cert expiry daily via TLS dial and displays status color-coded: green > 30d, amber 14–30d, red < 14d.

### Support Tickets
Customer opens a ticket; Blake gets an email. Replies go back into the thread from the portal UI. No third-party helpdesk.

## Deployment

### Prerequisites

- Linux server (amd64 or arm64)
- nginx
- An `arcline` system user

### Build

```sh
# Local binary
make build

# Cross-compile for Linux
make linux-amd64
make linux-arm64
```

### Install

```sh
# Create directories and user
sudo useradd -r -s /sbin/nologin -d /opt/arcline-portal arcline
sudo mkdir -p /opt/arcline-portal
sudo chown arcline:arcline /opt/arcline-portal

# Copy binary
sudo cp arcline-portal-linux-amd64 /opt/arcline-portal/arcline-portal
sudo chmod +x /opt/arcline-portal/arcline-portal

# Copy and populate env file
sudo cp .env.example /opt/arcline-portal/.env
sudo chown arcline:arcline /opt/arcline-portal/.env
sudo chmod 600 /opt/arcline-portal/.env
# Edit /opt/arcline-portal/.env and fill in real values
```

### systemd

```sh
sudo cp deploy/arcline-portal.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now arcline-portal
sudo systemctl status arcline-portal
```

### nginx

```sh
sudo cp deploy/nginx-portal.conf /etc/nginx/sites-available/arcline-portal
sudo ln -s /etc/nginx/sites-available/arcline-portal /etc/nginx/sites-enabled/
sudo nginx -t && sudo systemctl reload nginx
```

Expects TLS certificates at:
- `/etc/ssl/arclineit.com/fullchain.pem`
- `/etc/ssl/arclineit.com/privkey.pem`

### Seed first admin account

```sh
sudo -u arcline /opt/arcline-portal/arcline-portal \
  -seed \
  -username blake \
  -name "Blake" \
  -password "changeme"
```

## Environment variables

Copy `.env.example` to `.env` and set the following:

| Variable | Default | Description |
|---|---|---|
| `PORT` | `8082` | HTTP listen port (nginx proxies to this) |
| `DB_PATH` | `./portal.db` | Path to the portal SQLite database |
| `UPTIME_DB_PATH` | `../arcline-uptime/uptime.db` | Path to arcline-uptime's database (read-only); omit if not using uptime integration |
| `SESSION_SECRET` | — | 32-byte hex secret for session tokens. Generate with: `openssl rand -hex 32` |
| `SMTP_HOST` | `mail.arclineit.com` | SMTP server hostname |
| `SMTP_PORT` | `587` | SMTP port (STARTTLS) |
| `SMTP_USER` | — | SMTP username |
| `SMTP_PASS` | — | SMTP password |
| `SMTP_FROM` | `portal@arclineit.com` | From address for outbound email |
| `ADMIN_EMAIL` | `blake@arclineit.com` | Receives new ticket notifications |
| `BASE_URL` | `https://portal.arclineit.com` | Base URL used in email links (no trailing slash) |

## License

MIT — see [LICENSE](LICENSE).
