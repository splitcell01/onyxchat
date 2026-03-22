# OnyxChat Backend (Go)

Backend service for **OnyxChat**, a real-time encrypted messaging platform built with Go, PostgreSQL, Redis, and WebSockets.

This service handles **authentication**, **REST APIs**, **end-to-end encrypted messaging**, and **realtime WebSocket connections**.

---

## Part of the OnyxChat Platform

| Repo | Role |
|------|------|
| [splitcell01/web](https://github.com/splitcell01/web) | React / PWA frontend |
| [splitcell01/secure-messenger-server](https://github.com/splitcell01/secure-messenger-server) | This repo — Go backend |
| [splitcell01/onyxchat-iac](https://github.com/splitcell01/onyxchat-iac) | Terraform / AWS infrastructure |
| [splitcell01/batchq](https://github.com/splitcell01/batchq) | Async job queue (planned) |

---

## Architecture

```text
  Porkbun (registrar)
       │ nameservers delegated to Cloudflare
       ▼
  Cloudflare DNS (onyxchat.dev)
       │
       ├── onyxchat.dev / www  ──► Cloudflare Pages  (React PWA, static)
       │                           proxied, TLS managed by Cloudflare
       │
       └── api.onyxchat.dev   ──► AWS ALB  (Go backend)
                                   DNS only / grey cloud — no Cloudflare proxy
                                   TLS via ACM cert on ALB
                                        │
                              ┌─────────┴─────────┐
                              ▼                   ▼
                     ECS Fargate (Go)      ECS Fargate (Go)
                     task 1                task 2
                              │
             ┌────────────────┼──────────────────┐
             ▼                ▼                  ▼
    ┌────────────────┐  ┌────────────────┐  ┌────────────────┐
    │  RDS Postgres  │  │  ElastiCache   │  │  CloudWatch    │
    │  (messages,    │  │  Redis 7       │  │  Logs          │
    │   users,       │  │  (pub/sub,     │  │  90d retention │
    │   invite codes)│  │   WS tickets)  │  └────────────────┘
    └────────────────┘  └────────────────┘
                        ┌────────────────┐
                        │  SSM Parameter │
                        │  Store         │
                        │  (secrets)     │
                        └────────────────┘
```

### Frontend Hosting

The React PWA is static — it requires no application server. It is served via **Cloudflare Pages**:

- Deploys automatically on push to `main` from the [web repo](https://github.com/splitcell01/web)
- Cloudflare CDN distributes globally with HTTPS
- `VITE_API_URL=https://api.onyxchat.dev` points the client at this backend

No Nginx, Node, or separate web server is needed.

> **Note:** `api.onyxchat.dev` must be set to **DNS only (grey cloud)** in Cloudflare — not proxied. The ALB handles TLS directly via ACM, and Cloudflare's proxy would interfere with WebSocket upgrades.

### Realtime Flow

```text
Client                          Server                    Redis
  │                               │                         │
  ├─ POST /api/v1/ws/ticket ──────►                         │
  │   (Authorization: Bearer JWT) │                         │
  │                               ├─ SET ws:ticket:<id> ────►
  │                               │   TTL=30s               │
  ◄── { ticket: "<id>" } ─────────┤                         │
  │                               │                         │
  ├─ WS /api/v1/ws?ticket=<id> ───►                         │
  │                               ├─ GETDEL ws:ticket:<id> ─►
  │                               │◄── userID:username ─────┤
  ◄══ WebSocket established ══════┤   (key deleted)         │
```

Tickets are one-time use and expire in 30 seconds. The JWT never appears in WebSocket URLs or server logs.

---

## API Reference

### Auth

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `POST` | `/api/v1/register` | — | Register with invite code |
| `POST` | `/api/v1/login` | — | Login, receive JWT |

### Messaging

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `GET` | `/api/v1/users` | JWT | List all users |
| `POST` | `/api/v1/messages` | JWT | Send a message |
| `GET` | `/api/v1/messages?peer=<username>` | JWT | Fetch message history |

### E2E Encryption Keys

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `PUT` | `/api/v1/keys` | JWT | Upload your ECDH public key |
| `GET` | `/api/v1/keys/{username}` | JWT | Fetch a user's public key |

### WebSocket

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `POST` | `/api/v1/ws/ticket` | JWT | Get a one-time WS ticket |
| `GET` | `/api/v1/ws?ticket=<id>` | ticket | Open WebSocket connection |

### Health

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/health/live` | Liveness probe |
| `GET` | `/health/ready` | Readiness probe (checks DB) |

---

## End-to-End Encryption

Messages are encrypted client-side using **ECDH P-256 + AES-256-GCM** via the browser's Web Crypto API.

- Each user generates a keypair on first login, stored in IndexedDB (private key is non-extractable)
- The public key is uploaded to the server after login
- Before sending, the client derives a shared AES key via ECDH and encrypts the message body
- The server stores and relays ciphertext — it never sees plaintext
- Fallback to plaintext if a peer has not yet uploaded a public key

---

## Key Design Decisions

**Invite-only registration** — all registrations require a valid single-use invite code. Codes are created directly in the database and support optional expiry.

**Stateless backend** — no in-memory session state. JWT authentication enables horizontal scaling across multiple ECS tasks.

**Durable messaging** — messages are written to PostgreSQL before fanout. Offline users receive missed messages on reconnect via `sinceId`.

**Idempotent sends** — clients generate a `clientMessageId` per message. The server uses `ON CONFLICT` to deduplicate retries safely.

**Redis pub/sub for multi-instance fanout** — each ECS task subscribes to a Redis channel. Messages published by any instance are delivered to WebSocket clients on any instance.

**Deployment circuit breaker** — ECS automatically rolls back a deployment if the new task fails health checks.

---

## Local Development

### Prerequisites

- Go 1.25+
- Docker (for Postgres and Redis)

### Start dependencies

```bash
docker compose up -d
```

### Run the server

```bash
go run ./cmd/server
```

### Environment variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `SM_ENV` | no | `dev` | Set to `prod` to enable prod guards |
| `SM_SERVER_ADDR` | no | `:8080` | Listen address |
| `SM_DB_DSN` | yes | — | Postgres DSN |
| `SM_REDIS_ADDR` | no | `redis:6379` | Redis address |
| `SM_REDIS_AUTH_TOKEN` | prod only | — | Redis auth token (prod) |
| `JWT_SECRET` | prod only | insecure default | JWT signing secret |
| `SM_ALLOWED_ORIGINS` | prod only | — | Comma-separated CORS origins |

### Run tests

```bash
go test ./...
```

---

## Deployment

Deployment is fully automated via GitHub Actions on push to `main`:

1. Tests and coverage run
2. Docker image is built and pushed to ECR
3. ECS service is updated with the new image
4. GitHub Actions waits for the deployment to stabilise

Infrastructure is managed in [onyxchat-iac](https://github.com/splitcell01/onyxchat-iac) via Terraform.

---

## Status

| Feature | Status |
|---------|--------|
| Invite-only registration | ✅ |
| JWT authentication | ✅ |
| REST messaging API | ✅ |
| WebSocket realtime | ✅ |
| E2E encryption (ECDH + AES-GCM) | ✅ |
| Redis pub/sub fanout | ✅ |
| Secure WS ticket auth | ✅ |
| ECS Fargate deployment | ✅ |
| CI/CD via GitHub Actions | ✅ |
| Async job processing (BatchQ) | 🟡 planned |
| Push notifications | 🟡 planned |

---

## License

MIT# CI/CD enabled


