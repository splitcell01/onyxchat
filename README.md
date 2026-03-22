# OnyxChat

> A production-grade, end-to-end encrypted messaging platform built from scratch.  
> Live at **[onyxchat.dev](https://onyxchat.dev)**

---

## What is this?

OnyxChat is a real-time encrypted chat application I designed and built end-to-end — from cryptography and WebSocket infrastructure to cloud deployment and CI/CD. It is not a tutorial project. Every component is running in production.

Messages are encrypted in the browser before they leave your device. The server stores and relays ciphertext. Even I cannot read your messages.

---

## Architecture

```
Client (React PWA)
    │
    ├── onyxchat.dev ────────► Cloudflare Pages (CDN, global edge)
    │
    └── api.onyxchat.dev ────► AWS ALB (TLS via ACM)
                                    │
                          ┌─────────┴──────────┐
                          ▼                    ▼
                   ECS Fargate (Go)     ECS Fargate (Go)
                   task 1               task 2
                          │
          ┌───────────────┼────────────────┐
          ▼               ▼                ▼
   RDS PostgreSQL   ElastiCache Redis   CloudWatch Logs
   (users, messages, (pub/sub fanout,   (90d retention)
    invite codes)    WS tickets)
```

Two ECS tasks run behind a load balancer. Redis pub/sub fans out WebSocket messages across instances. PostgreSQL persists all messages. Secrets live in SSM Parameter Store — never in environment variables or code.

---

## End-to-End Encryption

Implemented using the **Web Crypto API** — no third-party crypto libraries.

```
Alice                                          Bob
  │                                             │
  ├─ generates ECDH P-256 keypair               ├─ generates ECDH P-256 keypair
  ├─ uploads public key to server               ├─ uploads public key to server
  │                                             │
  ├─ fetches Bob's public key                   │
  ├─ derives shared AES-256-GCM key via ECDH    │
  ├─ encrypts message with random 12-byte IV    │
  ├─ sends ciphertext + IV to server            │
  │                                             │
  │         server stores & relays ciphertext   │
  │                                             │
  │                          fetches Alice's public key ─┤
  │                    derives same shared AES key ──────┤
  │                         decrypts message in browser ─┤
```

Private keys are stored in IndexedDB as non-extractable `CryptoKey` objects. The server never sees plaintext.

---

## Tech Stack

| Layer | Technology |
|-------|-----------|
| Frontend | React 18, TypeScript, Vite, Web Crypto API |
| Backend | Go 1.25, gorilla/mux, gorilla/websocket |
| Database | PostgreSQL 17 (RDS) |
| Cache / Pub-Sub | Redis 7 (ElastiCache) |
| Infrastructure | AWS ECS Fargate, ALB, ACM, SSM, ECR |
| Frontend Hosting | Cloudflare Pages |
| IaC | Terraform |
| CI/CD | GitHub Actions (OIDC, no long-lived AWS keys) |
| Desktop Client | Java 21, JavaFX |

---

## Features

- ✅ End-to-end encryption (ECDH P-256 + AES-256-GCM)
- ✅ Real-time messaging over WebSocket
- ✅ Presence (online/offline status)
- ✅ Typing indicators
- ✅ Invite-only registration
- ✅ JWT authentication with secure WS ticket exchange
- ✅ Redis pub/sub fanout across multiple backend instances
- ✅ Idempotent message delivery (client-generated IDs, `ON CONFLICT`)
- ✅ Offline message catch-up on reconnect (`sinceId`)
- ✅ Per-user and per-IP rate limiting
- ✅ Automated CI/CD — push to main, live in 90 seconds
- ✅ Java desktop client

---

## WebSocket Authentication

JWTs never appear in WebSocket URLs or server logs. A one-time ticket system is used instead:

```
1. Client POSTs to /api/v1/ws/ticket with Authorization: Bearer <jwt>
2. Server stores ticket in Redis with 30s TTL
3. Client opens WS: /api/v1/ws?ticket=<id>
4. Server does GETDEL on ticket — one use, then gone
5. WebSocket is established
```

---

## CI/CD Pipeline

No manual deployments. Every push to `main` triggers the appropriate pipeline:

**Web** (`web/**` changed):
```
push → npm ci → vite build → wrangler pages deploy → live on Cloudflare
```

**Server** (`server/**` changed):
```
push → docker build → ECR push → ECS update-service → rolling deploy
```

AWS credentials use **OIDC** — no long-lived access keys stored anywhere.

---

## Repository Structure

```
onyxchat/
├── server/          # Go backend (API, WebSocket, E2EE, Redis pub/sub)
├── web/             # React PWA frontend
├── iac/             # Terraform (ECS, RDS, Redis, ALB, IAM, SSM)
└── desktop/         # Java/JavaFX desktop client
```

---

## Running Locally

```bash
# Backend
cd server/secure-messenger-server
docker compose up -d        # starts Postgres + Redis
go run ./cmd/server

# Frontend
cd web/onyxchat-web
npm install
npm run dev
```

---

## What I Learned Building This

- Implementing cryptographic protocols correctly is harder than using a library — understanding ECDH key derivation, IV uniqueness, and GCM authentication tags at the byte level
- WebSocket scaling across stateless instances requires a pub/sub layer — HTTP is easy to scale, persistent connections are not
- Production debugging: chasing schema drift between code and a live RDS instance column by column
- OIDC-based CI/CD is strictly better than storing AWS credentials as GitHub secrets
- Cloudflare's proxy must be bypassed for WebSocket connections to an ALB — TLS termination cannot be double-proxied

---

## Status

Live at [onyxchat.dev](https://onyxchat.dev). Invite-only during beta.

---

## License

MIT