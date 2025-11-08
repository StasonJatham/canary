<div align="center">
  <img src="docs/canary.jpg" alt="Canary Logo" width="200"/>
</div>

# Canary - Certificate Transparency Monitor

Real-time phishing detection through Certificate Transparency log monitoring with rule-based detection.

<div align="center">
  <img src="web/preview.jpg" alt="Canary Dashboard Preview" width="800"/>
</div>

## What

Canary monitors Certificate Transparency (CT) logs to identify potentially malicious domains as certificates are issued. It uses powerful rule-based detection with Boolean logic to catch sophisticated phishing campaigns targeting your brands.

**Key Features:**
- **Rule-Based Detection** - Complex patterns like `(paypal OR stripe) AND login NOT official`
- **High Performance** - Aho-Corasick algorithm with automatic keyword extraction
- **Priority System** - Classify threats (critical, high, medium, low)
- **Web Dashboard** - View matches and manage rules with authentication
- **REST API** - Complete API with OpenAPI documentation
- **Production Ready** - Docker setup with Certspotter integration

## Why

**Use Cases:**
- Brand protection and phishing detection
- Security monitoring for corporate domains
- Research on certificate issuance patterns
- Real-time threat intelligence gathering

**Benefits:**
- Catch phishing domains as certificates are issued (hours before DNS propagation)
- Sub-millisecond matching with minimal resource usage
- Flexible rules without code changes
- Time-based partitioning for efficient queries
- Automatic cleanup of old data

## How to Deploy

### Option 1: Docker (Recommended)

Complete production setup with automatic CT log monitoring:

```bash
cd deployments/docker
docker-compose up -d --build
```


This includes:
- Canary service (port 8080)
- Certspotter monitoring 40+ CT logs
- SQLite database with automatic partitioning
- Web dashboard and API

**First Login:**

When you first start Canary, it creates a random admin user and displays the credentials in the logs:

```bash
# View the auto-generated credentials
docker-compose logs canary | grep "INITIAL USER CREATED" -A 5
```

Example output:
```
========================================
INITIAL USER CREATED
Username: admin_a1b2c3d4
Password: xK9$mP2@qL5#vR8n
Please save these credentials!
Session expires after 30 days
========================================
```

Access the dashboard at http://localhost:8080 and log in with these credentials.

**Access Services:**
```bash
# Web Dashboard (requires login)
open http://localhost:8080

# API Documentation
open http://localhost:8080/docs

# Health Check (public)
curl http://localhost:8080/health
```

**View Logs:**
```bash
docker-compose logs -f canary
docker-compose logs -f certspotter
```

**Stop Services:**
```bash
docker-compose down
```

### Option 2: Local Development

```bash
# Build
go build -o canary ./cmd/canary

# Run (creates initial user on first start)
./canary
```

The service runs on port 8080 (override with `PORT=3000`).

## Authentication

Canary uses session-based authentication. Sessions are valid for 30 days.

### Initial User

On first startup, Canary automatically creates a random admin user. The credentials are displayed in the console output. Save these credentials immediately!

### Creating Additional Users

Use the provided script to create or update users directly in the database:

```bash
# Create a new user
go run scripts/create_user.go -username admin -password yourpassword

# Create with custom database path
go run scripts/create_user.go -username admin -password yourpassword -db /path/to/matches.db

# Update existing user's password (will prompt for confirmation)
go run scripts/create_user.go -username admin -password newpassword
```

**Script Usage:**
```bash
Usage: go run scripts/create_user.go -username <username> -password <password> [-db <db_path>]

Example:
  go run scripts/create_user.go -username admin -password mypassword
  go run scripts/create_user.go -username admin -password mypassword -db /path/to/matches.db
```

**Note:** There is no `/auth/create-user` API endpoint. Users can only be created:
1. Automatically on first startup
2. Using the `create_user.go` script

### Login & Logout

**Login via API:**
```bash
curl -X POST http://localhost:8080/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username": "admin", "password": "yourpassword"}' \
  -c cookies.txt
```

**Logout:**
```bash
curl -X POST http://localhost:8080/auth/logout -b cookies.txt
```

**Using the Web UI:**
- Navigate to http://localhost:8080
- Enter your credentials
- Click logout button in the top right corner

## Detection Rules

### Configuration

Rules are defined in `data/rules.yaml` with Boolean logic support:

```yaml
rules:
  - name: paypal-phishing
    keywords: paypal AND (login OR secure OR account) NOT paypal.com
    priority: critical
    enabled: true
    comment: "Detect PayPal phishing domains"

  - name: tech-brands
    keywords: (google OR microsoft OR apple) AND (verify OR signin)
    priority: high
    enabled: true
    comment: "Monitor major tech brands"

  - name: cloud-providers
    keywords: aws OR azure OR gcp
    priority: medium
    enabled: false
    comment: "Cloud provider monitoring (disabled)"
```

**Rule Fields:**
- `name` - Unique identifier
- `keywords` - Boolean expression (AND, OR, NOT with parentheses)
- `priority` - Threat level: `critical`, `high`, `medium`, `low`
- `enabled` - Enable/disable without deletion
- `comment` - Description shown in UI

### Managing Rules

**Reload from File:**
```bash
curl -X POST http://localhost:8080/rules/reload
```

**Via API:**
```bash
# List all rules
curl http://localhost:8080/rules

# Create rule
curl -X POST http://localhost:8080/rules/create \
  -H "Content-Type: application/json" \
  -d '{
    "name": "stripe-phishing",
    "keywords": "stripe AND payment",
    "priority": "high",
    "enabled": true,
    "comment": "Stripe phishing detection"
  }'

# Update rule
curl -X PUT http://localhost:8080/rules/update/stripe-phishing \
  -H "Content-Type: application/json" \
  -d '{
    "name": "stripe-phishing",
    "keywords": "stripe AND (payment OR checkout)",
    "priority": "critical",
    "enabled": true,
    "comment": "Updated Stripe detection"
  }'

# Delete rule
curl -X DELETE http://localhost:8080/rules/delete/stripe-phishing

# Toggle rule (enable/disable)
curl -X PUT http://localhost:8080/rules/toggle/stripe-phishing
```

**Via Web UI:**
- Navigate to the Rules tab in the dashboard
- Use the UI to create, edit, toggle, or delete rules
- Changes are immediately reflected

## API Usage

### Authentication

Most endpoints require authentication via session cookie. Login first to obtain a session.

**Get Session Cookie:**
```bash
curl -X POST http://localhost:8080/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username": "admin", "password": "yourpassword"}' \
  -c cookies.txt

# Use the cookie in subsequent requests
curl http://localhost:8080/matches -b cookies.txt
```

### Endpoints

**Public Endpoints:**
- `GET /login` - Login page
- `POST /auth/login` - Authenticate user
- `POST /hook` - Certspotter webhook (no auth required)
- `GET /health` - Health check

**Protected Endpoints (require authentication):**

#### Matches

```bash
# Get recent matches from memory
curl http://localhost:8080/matches -b cookies.txt | jq

# Get matches from database (last 24 hours)
curl "http://localhost:8080/matches/recent?minutes=1440" -b cookies.txt | jq

# With pagination (50 per page)
curl "http://localhost:8080/matches/recent?minutes=1440&limit=50&offset=0" -b cookies.txt | jq

# Clear in-memory cache
curl -X POST http://localhost:8080/matches/clear -b cookies.txt
```

**Response Example:**
```json
{
  "count": 50,
  "total": 1234,
  "limit": 50,
  "offset": 0,
  "has_more": true,
  "matches": [
    {
      "dns_names": ["paypal-secure.com", "*.paypal-secure.com"],
      "matched_domains": ["paypal", "secure"],
      "matched_rule": "paypal-phishing",
      "priority": "critical",
      "tbs_sha256": "abc123...",
      "cert_sha256": "def456...",
      "detected_at": "2025-11-07T10:30:00Z"
    }
  ]
}
```

#### Rules

```bash
# List rules
curl http://localhost:8080/rules -b cookies.txt | jq

# Create rule
curl -X POST http://localhost:8080/rules/create -b cookies.txt \
  -H "Content-Type: application/json" \
  -d '{
    "name": "banking-phish",
    "keywords": "(bank OR chase OR wellsfargo) AND login",
    "priority": "high",
    "enabled": true,
    "comment": "Banking phishing"
  }'

# Update rule (use PUT)
curl -X PUT http://localhost:8080/rules/update/banking-phish -b cookies.txt \
  -H "Content-Type: application/json" \
  -d '{
    "name": "banking-phish",
    "keywords": "(bank OR chase OR wellsfargo) AND (login OR signin)",
    "priority": "critical",
    "enabled": true,
    "comment": "Enhanced banking phishing"
  }'

# Delete rule (use DELETE)
curl -X DELETE http://localhost:8080/rules/delete/banking-phish -b cookies.txt

# Toggle rule (use PUT)
curl -X PUT http://localhost:8080/rules/toggle/banking-phish -b cookies.txt

# Reload from file
curl -X POST http://localhost:8080/rules/reload -b cookies.txt
```

#### Metrics

```bash
# System metrics
curl http://localhost:8080/metrics -b cookies.txt | jq

# Performance metrics
curl "http://localhost:8080/metrics/performance?minutes=60" -b cookies.txt | jq
```

**Metrics Response:**
```json
{
  "queue_len": 0,
  "total_matches": 142,
  "total_certs": 15000,
  "watched_domains": 37,
  "rules_count": 5,
  "uptime_seconds": 86400,
  "recent_matches": 10
}
```

### Webhook Testing

Test the webhook endpoint manually:

```bash
curl -X POST http://localhost:8080/hook \
  -H "Content-Type: application/json" \
  -d '{
    "id": "test-cert-001",
    "issuance": {
      "dns_names": [
        "paypal-secure-login.com",
        "www.paypal-secure-login.com"
      ],
      "tbs_sha256": "abc123def456...",
      "cert_sha256": "789xyz012..."
    }
  }'
```

### Full OpenAPI Documentation

Interactive API documentation with all endpoints, schemas, and examples:

**http://localhost:8080/docs**

## Configuration

### Environment Variables

```bash
PORT=8080                        # HTTP port (default: 8080)
DEBUG=true                       # Enable debug logging (default: false)
PUBLIC_DASHBOARD=true            # Make dashboard public (read-only, no login required)
DOMAIN=canary.yourdomain.com     # Domain for HTTPS/reverse proxy (enables secure cookies)
PARTITION_RETENTION_DAYS=30      # Days to keep data (default: 30)
CLEANUP_INTERVAL_HOURS=24        # Hours between cleanups (default: 24)
```

**Docker Example:**
```yaml
# docker-compose.yml
environment:
  - PORT=8080
  - DEBUG=true
  - PUBLIC_DASHBOARD=true          # Public read-only access
  - DOMAIN=canary.yourdomain.com   # Enables HTTPS mode
  - PARTITION_RETENTION_DAYS=60
  - CLEANUP_INTERVAL_HOURS=12
```

### Reverse Proxy Setup (Caddy/nginx)

When running behind a reverse proxy with HTTPS, set the `DOMAIN` environment variable. This automatically:
- Enables secure cookies (`Secure` flag)
- Sets CORS origin to `https://yourdomain.com`
- Enables `Access-Control-Allow-Credentials` for cookie-based auth

**Why `DOMAIN` is needed:**
- Browsers require `Secure` flag on cookies when using HTTPS
- Cookie-based authentication needs proper CORS with credentials
- Without it, login will fail when accessed via HTTPS

#### Caddy Setup

**1. Create Caddyfile:**
```caddy
canary.yourdomain.com {
    reverse_proxy canary:8080 {
        header_up X-Real-IP {remote_host}
        header_up X-Forwarded-For {remote_host}
        header_up X-Forwarded-Proto {scheme}
    }

    header {
        Strict-Transport-Security "max-age=31536000; includeSubDomains"
        X-Frame-Options "SAMEORIGIN"
        X-Content-Type-Options "nosniff"
    }
}
```

**2. Update docker-compose.yml:**
```yaml
services:
  caddy:
    image: caddy:2-alpine
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - ./Caddyfile:/etc/caddy/Caddyfile
      - caddy_data:/data
      - caddy_config:/config
    networks:
      - canary-network

  canary:
    environment:
      - DOMAIN=canary.yourdomain.com  # Enable HTTPS mode
    expose:
      - "8080"  # Internal only, not published
    networks:
      - canary-network

volumes:
  caddy_data:
  caddy_config:
```

**3. Start services:**
```bash
docker-compose up -d
```

Caddy automatically obtains Let's Encrypt certificates and handles HTTPS.

See `deployments/Caddyfile.example` for a complete configuration.

#### nginx Setup

**1. Create nginx.conf:**
```nginx
server {
    listen 80;
    server_name canary.yourdomain.com;
    return 301 https://$server_name$request_uri;
}

server {
    listen 443 ssl http2;
    server_name canary.yourdomain.com;

    ssl_certificate /etc/letsencrypt/live/canary.yourdomain.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/canary.yourdomain.com/privkey.pem;

    location / {
        proxy_pass http://canary:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

**2. Set DOMAIN in docker-compose.yml:**
```yaml
canary:
  environment:
    - DOMAIN=canary.yourdomain.com
```

### Public Dashboard Mode

Set `PUBLIC_DASHBOARD=true` to make the dashboard accessible without authentication for viewing only.

**What it does:**
- ✅ Dashboard and rules are viewable without login
- ✅ Matches, metrics, and API docs are publicly accessible
- ❌ Editing, creating, deleting rules requires authentication
- ❌ Clearing matches requires authentication

**Use cases:**
- Share phishing detection results with your team
- Public threat intelligence dashboard
- Security operations center (SOC) displays
- Transparency for stakeholders

**Example:**
```yaml
# docker-compose.yml
environment:
  - PUBLIC_DASHBOARD=true
```

**Security notes:**
- Anyone can view matches and rules
- Modifications still require login
- Consider if your matched domains contain sensitive information
- Use `DOMAIN` with public dashboard for proper CORS

**How it works:**
- GET requests allowed without auth (viewing)
- POST/PUT/DELETE require authentication (editing)
- UI automatically hides edit buttons when not authenticated
- `/config` endpoint tells UI if public mode is enabled

### Data Persistence

The `data/` directory contains:
- `rules.yaml` - Detection rules
- `matches.db` - SQLite database with match history

**Docker Volume:**
```yaml
volumes:
  - ./data:/app/data  # Persistent storage
```

## Integration Examples

### Slack Notifications

```bash
#!/bin/bash
# check-canary.sh

MATCHES=$(curl -s http://localhost:8080/matches/recent?minutes=5 -b cookies.txt)
COUNT=$(echo "$MATCHES" | jq '.count')

if [ "$COUNT" -gt 0 ]; then
  MESSAGE=$(echo "$MATCHES" | jq -r '.matches[] | "Priority: \(.priority) - Rule: \(.matched_rule) - Domains: \(.dns_names | join(", "))"')

  curl -X POST https://hooks.slack.com/services/YOUR/WEBHOOK/URL \
    -H "Content-Type: application/json" \
    -d "{\"text\": \"Canary Alert: $COUNT new matches\n$MESSAGE\"}"
fi
```

### Email Alerts

```bash
#!/bin/bash
# email-alerts.sh

MATCHES=$(curl -s http://localhost:8080/matches/recent?minutes=60 -b cookies.txt)
COUNT=$(echo "$MATCHES" | jq '.count')

if [ "$COUNT" -gt 0 ]; then
  echo "$MATCHES" | jq -r '.matches[] | "Rule: \(.matched_rule)\nPriority: \(.priority)\nDomains: \(.dns_names | join(", "))\n---"' | \
    mail -s "Canary Alert: $COUNT new matches" security@company.com
fi
```

### Continuous Monitoring

```bash
# Add to crontab (runs every 5 minutes)
*/5 * * * * /path/to/check-canary.sh
```

## How It Works

1. **Certspotter** monitors 40+ Certificate Transparency logs
2. **Webhook** sends new certificates to Canary's `/hook` endpoint
3. **Keyword Extraction** - Rules are parsed and keywords automatically extracted
4. **Matching** - Aho-Corasick algorithm matches domains in O(n+m) time
5. **Rule Evaluation** - Boolean logic evaluated (priority order, first match wins)
6. **Storage** - Matches stored in time-partitioned SQLite tables
7. **Cleanup** - Old partitions automatically deleted based on retention policy
8. **API/UI** - Access via authenticated REST API or web dashboard

**Performance:**
- Sub-millisecond matching per certificate
- Thousands of certificates per second
- Minimal memory footprint
- Efficient time-based queries

## Troubleshooting

**Can't login:**
```bash
# Check initial user credentials in logs
docker-compose logs canary | grep "INITIAL USER"

# Or create new user
go run scripts/create_user.go -username newadmin -password newpass
```

**No matches appearing:**
```bash
# Check rules loaded
curl http://localhost:8080/rules

# Check Certspotter running
docker-compose ps

# View Certspotter logs
docker-compose logs certspotter

# Reload rules
curl -X POST http://localhost:8080/rules/reload
```

**API connection errors:**
```bash
# Check health
curl http://localhost:8080/health

# Check you're authenticated
curl http://localhost:8080/matches -b cookies.txt

# View logs
docker-compose logs canary
```

**Database issues:**
```bash
# Check database exists
ls -lh data/matches.db

# Check permissions
ls -ld data/

# Check disk space
df -h
```

## Development

**Build:**
```bash
go build -o canary ./cmd/canary
```

**Requirements:**
- Go 1.21+
- CGO enabled (for SQLite)
- GCC/musl-dev

**Dependencies:**
```bash
go mod download
```

**Test:**
```bash
go test ./...
```

## License

For authorized security research and defensive purposes only.

## Third-Party Licenses

Canary uses the following open-source dependencies. We acknowledge and are grateful to the developers for their contributions:

### Certspotter

**License:** Mozilla Public License 2.0 (MPL-2.0)
**Source:** https://github.com/SSLMate/certspotter
**Copyright:** Copyright © SSLMate, Inc.

This project uses Certspotter for monitoring Certificate Transparency logs. Certspotter is licensed under the Mozilla Public License 2.0. The source code is available at the repository above, and we build it from source in our Docker deployment. The full MPL-2.0 license is available at http://mozilla.org/MPL/2.0/.

### go-sqlite3

**License:** MIT License
**Source:** https://github.com/mattn/go-sqlite3
**Copyright:** Copyright (c) 2014 Yasuhiro Matsumoto

### ahocorasick

**License:** MIT License
**Source:** https://github.com/anknown/ahocorasick
**Copyright:** Copyright (c) 2015 hanshinan

### gopsutil

**License:** BSD 3-Clause License
**Source:** https://github.com/shirou/gopsutil
**Copyright:** Copyright (c) 2014, WAKAYAMA Shirou

### minify

**License:** MIT License
**Source:** https://github.com/tdewolff/minify
**Copyright:** Copyright (c) 2025 Taco de Wolff

### yaml.v3

**License:** MIT License and Apache License 2.0
**Source:** https://github.com/go-yaml/yaml
**Copyright:** Copyright (c) 2006-2011 Kirill Simonov, Copyright (c) 2011-2019 Canonical Ltd

### golang.org/x/crypto

**License:** BSD 3-Clause License
**Source:** https://cs.opensource.google/go/x/crypto
**Copyright:** Copyright 2009 The Go Authors

---

**Full License Texts:**

The full text of each license can be found in the respective project repositories linked above. All licenses require that copyright notices be retained in redistributions. We comply with all license requirements and acknowledge the original authors.
