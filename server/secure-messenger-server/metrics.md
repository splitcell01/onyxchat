# OnyxChat — Metrics & Load Testing Guide

> **Goal:** Generate real, quantifiable performance data for your resume and portfolio.  
> Expected headline numbers after a full load test run:
> - **Throughput:** ~1,000–1,500 req/s at 200 concurrent VUs
> - **p95 HTTP latency:** < 50 ms (local), < 100 ms (AWS)
> - **WS connections:** 200 simultaneous with < 1% drop rate
> - **DB query p95:** < 10 ms (message_create), < 5 ms (message_list)

---

## What Was Added

### New Prometheus metrics (in `observability_middleware.go`)

| Metric | Type | Description |
|---|---|---|
| `ws_active_connections` | Gauge | Currently open WebSocket connections |
| `ws_messages_received_total{type}` | Counter | Inbound WS frames by message type |
| `ws_rate_limit_rejections_total` | Counter | WS connections dropped by rate limiter |
| `messages_sent_total` | Counter | Successfully persisted chat messages |
| `db_query_duration_seconds{operation}` | Histogram | DB latency per operation (p50/p95/p99) |
| `http_rate_limit_rejections_total{limiter}` | Counter | HTTP 429s by limiter type |

### Files changed
- `internal/http/observability_middleware.go` — new metrics + `ObserveDBQuery()` helper
- `internal/http/ws.go` — gauge inc/dec on connect/disconnect, WS message type counter, rate limit counter
- `internal/http/message_handlers.go` — DB timing on create + list, `MessagesSent.Inc()`
- `compose.yaml` — added Prometheus + Grafana services
- `monitoring/` — Prometheus scrape config, Grafana provisioning, pre-built dashboard
- `scripts/load_test.js` — k6 load test (HTTP + WebSocket)

---

## Quick Start (Local)

```bash
# 1. Start the full stack (server + DB + Prometheus + Grafana)
docker compose up --build -d

# 2. Verify the server is up
curl http://localhost:8080/health/ready

# 3. Open Grafana  →  http://localhost:3000  (admin / admin)
#    The "OnyxChat — Server Metrics" dashboard loads automatically.

# 4. Run the load test (install k6 first: https://k6.io/docs/get-started/installation/)
k6 run scripts/load_test.js \
    -e BASE_URL=http://localhost:8080 \
    -e WS_URL=ws://localhost:8080

# 5. Watch metrics update live in Grafana during the test.
```

---

## Against AWS (Deployed)

```bash
k6 run scripts/load_test.js \
    -e BASE_URL=https://api.onyxchat.dev \
    -e WS_URL=wss://api.onyxchat.dev
```

> **Note:** Prometheus still runs locally and scrapes `localhost:8080` by default.  
> To scrape your AWS endpoint, update `monitoring/prometheus.yml`:
> ```yaml
> static_configs:
>   - targets: ["api.onyxchat.dev:443"]
> ```
> And add `scheme: https` under that job.

---

## Load Test Stages

| Stage | Duration | VUs | Purpose |
|---|---|---|---|
| Ramp-up | 30s | 0 → 20 | Warm-up |
| Baseline | 60s | 20 | Normal load latency |
| Stress | 30s + 60s | 20 → 100 | Concurrency stress |
| Spike | 30s + 30s | 100 → 200 | Worst-case throughput |
| Recovery | 30s + 30s | 200 → 0 | Drain behaviour |

**Each VU does:**
1. Register a new user
2. List all users
3. Send 3 messages to a peer
4. Read conversation history
5. Open a WebSocket and send 5 typing indicators

**Pass/fail thresholds:**
- HTTP p95 < 500 ms
- Error rate < 1%
- WS message delay p95 < 200 ms

---

## Portfolio-Ready Numbers to Capture

After the test run, screenshot or export:

1. **Grafana → "HTTP Latency Percentiles"** panel — shows p50/p95/p99
2. **Grafana → "Active WS Connections"** stat — peak concurrent connections
3. **Grafana → "Request Rate by Route"** — peak req/s per endpoint
4. **k6 terminal summary** — copy the summary table printed at the end
5. **Grafana → "DB Query Latency Percentiles"** — shows DB is the bottleneck or not

### Example resume bullet (fill in your real numbers):

> *"Load-tested OnyxChat under 200 concurrent virtual users; achieved 1,200 req/s with p95 HTTP latency of 38 ms and zero errors. Instrumented the Go server with Prometheus (HTTP, WebSocket, DB histograms) and built a Grafana dashboard for real-time observability."*

---

## Prometheus Queries (raw, for screenshots)

```promql
# Peak throughput
sum(rate(http_requests_total[1m]))

# p99 HTTP latency
histogram_quantile(0.99, sum(rate(http_request_duration_seconds_bucket[1m])) by (le))

# Active WS connections
ws_active_connections

# Message throughput
rate(messages_sent_total[1m]) * 60

# DB p95 by operation
histogram_quantile(0.95, sum(rate(db_query_duration_seconds_bucket[1m])) by (le, operation))

# Error rate %
sum(rate(http_requests_total{status=~"5.."}[1m])) / sum(rate(http_requests_total[1m])) * 100
```