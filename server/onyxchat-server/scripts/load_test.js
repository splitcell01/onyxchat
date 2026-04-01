/**
 * OnyxChat k6 Load Test
 * ─────────────────────
 * Usage:
 *
 *   # Local
 *   k6 run scripts/load_test.js \
 *       -e BASE_URL=http://localhost:8080 \
 *       -e WS_URL=ws://localhost:8080 \
 *       -e INVITE_PREFIX=k6- \
 *       -e SETUP_USERS=400
 *
 *   # Prod
 *   k6 run scripts/load_test.js \
 *       -e BASE_URL=https://api.onyxchat.dev \
 *       -e WS_URL=wss://api.onyxchat.dev \
 *       -e INVITE_PREFIX=k6- \
 *       -e SETUP_USERS=400
 *
 * What this tests (the real hot path):
 *   - JWT auth (fast crypto verify, not bcrypt)
 *   - Sending messages
 *   - Listing conversation history
 *   - WebSocket connections + typing indicators
 *   - Presence events
 *
 * Registration happens ONCE in setup() for a fixed pool of users.
 * VUs reuse those users across all iterations — no bcrypt in the hot path.
 *
 * Stages:
 *   Stage 1  — ramp to 20 VUs  (baseline)
 *   Stage 2  — hold 20 VUs     (steady state)
 *   Stage 3  — ramp to 100 VUs (stress)
 *   Stage 4  — hold 100 VUs    (stress steady state)
 *   Stage 5  — ramp to 200 VUs (spike)
 *   Stage 6  — hold 200 VUs    (spike hold)
 *   Stage 7  — ramp to 20 VUs  (recovery)
 *   Stage 8  — ramp to 0       (ramp-down)
 */

import http from "k6/http";
import ws   from "k6/ws";
import { check, sleep } from "k6";
import { Rate, Trend }  from "k6/metrics";
import { randomString } from "https://jslib.k6.io/k6-utils/1.4.0/index.js";

// ── Config ────────────────────────────────────────────────────────────────────

const BASE          = __ENV.BASE_URL      || "http://localhost:8080";
const WS_BASE       = __ENV.WS_URL        || "ws://localhost:8080";
const API           = `${BASE}/api/v1`;
const INVITE_PREFIX = __ENV.INVITE_PREFIX || "k6-";
const SETUP_USERS   = parseInt(__ENV.SETUP_USERS || "400", 10);

// ── Custom metrics ────────────────────────────────────────────────────────────

const errorRate      = new Rate("errors");
const wsMessageDelay = new Trend("ws_message_delay_ms", true);

// ── Load profile ──────────────────────────────────────────────────────────────

export const options = {
  setupTimeout: "3m",
  stages: [
    { duration: "30s", target: 20  },
    { duration: "60s", target: 20  },
    { duration: "30s", target: 100 },
    { duration: "60s", target: 100 },
    { duration: "30s", target: 200 },
    { duration: "30s", target: 200 },
    { duration: "30s", target: 20  },
    { duration: "30s", target: 0   },
  ],
  thresholds: {
    http_req_duration:   ["p(95)<500"],
    errors:              ["rate<0.10"],
    ws_message_delay_ms: ["p(95)<200"],
  },
};

// ── Setup: register user pool once ───────────────────────────────────────────

export function setup() {
  console.log(`Registering ${SETUP_USERS} users for load test pool...`);

  const users    = [];
  const password = "Loadtest1!";

  for (let i = 0; i < SETUP_USERS; i++) {
    const username   = `k6_${randomString(12)}`;
    const inviteCode = `${INVITE_PREFIX}${i + 1}`;

    const res = http.post(
      `${API}/register`,
      JSON.stringify({ username, password, invite_code: inviteCode }),
      { headers: { "Content-Type": "application/json" } }
    );

    if (res.status === 200) {
      try {
        const body = JSON.parse(res.body);
        if (body.token) users.push({ username, token: body.token });
      } catch (_) {}
    }

    sleep(0.01);
  }

  console.log(`Setup complete: ${users.length}/${SETUP_USERS} users registered`);

  if (users.length < 2) {
    throw new Error(`Not enough users registered (${users.length}). Check invite codes.`);
  }

  return { users };
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function sendMessage(token, recipientUsername) {
  const clientMessageId = `${__VU}-${__ITER}-${randomString(8)}`;

  const res = http.post(
    `${API}/messages`,
    JSON.stringify({
      recipientUsername,
      body: `k6 VU=${__VU} iter=${__ITER} ts=${Date.now()}`,
      clientMessageId,
    }),
    {
      headers: {
        "Content-Type": "application/json",
        Authorization:  `Bearer ${token}`,
      },
    }
  );

  const ok = check(res, {
    "send message ok": (r) => r.status === 201 || r.status === 200,
  });
  errorRate.add(!ok);
  return ok;
}

function listMessages(token, peerUsername) {
  const res = http.get(`${API}/messages?peer=${peerUsername}`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  const ok = check(res, { "list messages 200": (r) => r.status === 200 });
  errorRate.add(!ok);
}

function listUsers(token) {
  const res = http.get(`${API}/users`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  const ok = check(res, { "list users 200": (r) => r.status === 200 });
  errorRate.add(!ok);
}

function openWebSocket(token, peerUsername) {
  const ticketRes = http.post(
    `${API}/ws/ticket`,
    null,
    { headers: { Authorization: `Bearer ${token}` } }
  );

  const ticketOk = check(ticketRes, { "ws ticket 200": (r) => r.status === 200 });
  errorRate.add(!ticketOk);
  if (!ticketOk) return;

  let ticket;
  try {
    ticket = JSON.parse(ticketRes.body).ticket;
  } catch {
    errorRate.add(true);
    return;
  }

  const url = `${WS_BASE}/api/v1/ws?ticket=${encodeURIComponent(ticket)}`;

  const res = ws.connect(url, {
    headers: { "Origin": "https://onyxchat.dev" },
  }, (socket) => {
    socket.on("open", () => {
      for (let i = 0; i < 3; i++) {
        socket.send(
          JSON.stringify({ type: "typing", to: peerUsername, isTyping: i % 2 === 0 })
        );
      }
      socket.setTimeout(() => socket.close(), 1000);
    });

    socket.on("message", (data) => {
      try {
        const msg = JSON.parse(data);
        if (msg._ts) wsMessageDelay.add(Date.now() - msg._ts);
      } catch (_) {}
    });

    socket.setTimeout(() => socket.close(), 3000);
  });

  const ok = check(res, { "ws connected": (r) => r && r.status === 101 });
  errorRate.add(!ok);
}

// ── Default scenario ──────────────────────────────────────────────────────────

export default function (data) {
  const { users } = data;

  // Pick two different users from the pool based on VU index
  const selfIdx = (__VU - 1) % users.length;
  const peerIdx = __VU % users.length;
  const self    = users[selfIdx];
  const peer    = users[peerIdx];

  // 1. List users
  listUsers(self.token);
  sleep(0.1);

  // 2. Send 3 messages to peer
  for (let i = 0; i < 3; i++) {
    sendMessage(self.token, peer.username);
    sleep(0.05);
  }

  // 3. Read conversation history
  listMessages(self.token, peer.username);
  sleep(0.1);

  // 4. WebSocket typing indicators
  openWebSocket(self.token, peer.username);

  sleep(0.5);
}

// ── Teardown ──────────────────────────────────────────────────────────────────

export function teardown(data) {
  console.log(`Test complete. Pool size: ${data.users.length} users.`);
}

// ── Summary ───────────────────────────────────────────────────────────────────

export function handleSummary(data) {
  const p95    = (m) => data.metrics[m]?.values?.["p(95)"]?.toFixed(2) ?? "n/a";
  const errPct = ((data.metrics.errors?.values?.rate ?? 0) * 100).toFixed(2);

  console.log(`
╔══════════════════════════════════════════╗
║        OnyxChat Load Test Summary        ║
╠══════════════════════════════════════════╣
║  HTTP p95 latency : ${String(p95("http_req_duration") + " ms").padEnd(20)}║
║  WS msg p95 delay : ${String(p95("ws_message_delay_ms") + " ms").padEnd(20)}║
║  Error rate       : ${String(errPct + " %").padEnd(20)}║
║  Total requests   : ${String(data.metrics.http_reqs?.values?.count ?? "n/a").padEnd(20)}║
╚══════════════════════════════════════════╝
`);

  return { stdout: JSON.stringify(data, null, 2) };
}
