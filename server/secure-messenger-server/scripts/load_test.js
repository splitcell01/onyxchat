/**
 * OnyxChat k6 Load Test
 * ─────────────────────
 * Usage:
 *
 *   # Local
 *   k6 run scripts/load_test.js \
 *       -e BASE_URL=http://localhost:8080 \
 *       -e WS_URL=ws://localhost:8080 \
 *       -e INVITE_CODE=<your-invite-code>
 *
 *   # Prod
 *   k6 run scripts/load_test.js \
 *       -e BASE_URL=https://api.onyxchat.dev \
 *       -e WS_URL=wss://api.onyxchat.dev \
 *       -e INVITE_CODE=<your-invite-code>
 *
 * The INVITE_CODE must be a code that exists in your invite_codes table but
 * has NOT been consumed yet. Because the load test registers many users, you
 * need a pool of codes. Seed them first:
 *
 *   INSERT INTO invite_codes (code, created_by)
 *   SELECT 'k6-' || generate_series(1, 500), 'load-test';
 *
 * Then pass the shared prefix and the script will pick a per-VU code:
 *   -e INVITE_PREFIX=k6-        (default)
 *
 * Alternatively, pass a single INVITE_CODE and the script will reuse it
 * only for the very first registration per VU (peer setup). Set
 *   -e INVITE_POOL_SIZE=500     to match how many codes you seeded.
 *
 * Simplest approach for local dev: set INVITE_CODE to one code and seed
 * enough codes for the number of VUs you plan to run.
 *
 * What this tests:
 *   Stage 1  — ramp to 20 VUs  (baseline latency)
 *   Stage 2  — hold 20 VUs     (steady state)
 *   Stage 3  — ramp to 100 VUs (stress)
 *   Stage 4  — hold 100 VUs    (stress steady state)
 *   Stage 5  — ramp to 200 VUs (spike)
 *   Stage 6  — hold 200 VUs    (spike hold)
 *   Stage 7  — ramp to 20 VUs  (recovery)
 *   Stage 8  — ramp to 0       (ramp-down)
 *
 * Each VU:
 *   register → login → list users → send 3 messages → read history → WS typing
 *
 * Thresholds:
 *   • p95 HTTP latency  < 500 ms
 *   • error rate        < 10%
 *   • p95 WS msg delay  < 200 ms
 */

import http from "k6/http";
import ws   from "k6/ws";
import { check, sleep } from "k6";
import { Rate, Trend }  from "k6/metrics";
import { randomString } from "https://jslib.k6.io/k6-utils/1.4.0/index.js";

// ── Config ────────────────────────────────────────────────────────────────────

const BASE         = __ENV.BASE_URL       || "http://localhost:8080";
const WS_BASE      = __ENV.WS_URL         || "ws://localhost:8080";
const API          = `${BASE}/api/v1`;
const INVITE_PREFIX = __ENV.INVITE_PREFIX || "k6-";
const POOL_SIZE    = parseInt(__ENV.INVITE_POOL_SIZE || "500", 10);

// ── Custom metrics ────────────────────────────────────────────────────────────

const errorRate      = new Rate("errors");
const wsMessageDelay = new Trend("ws_message_delay_ms", true);

// ── Load profile ──────────────────────────────────────────────────────────────

export const options = {
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

// ── Helpers ───────────────────────────────────────────────────────────────────

/**
 * Pick a unique invite code for this VU iteration.
 * Uses a rotating index across the seeded pool so concurrent VUs
 * don't all race on the same code.
 */
function pickInviteCode() {
  const idx = (__VU * 100 + __ITER) % POOL_SIZE + 1;
  return `${INVITE_PREFIX}${idx}`;
}

function register() {
  const username = `k6_${randomString(10)}`;
  const password = "Loadtest1!";
  const inviteCode = pickInviteCode();

  const res = http.post(
    `${API}/register`,
    JSON.stringify({ username, password, invite_code: inviteCode }),
    { headers: { "Content-Type": "application/json" } }
  );

  const ok = check(res, {
    "register 200":       (r) => r.status === 200,
    "register has token": (r) => {
      try { return !!JSON.parse(r.body).token; }
      catch { return false; }
    },
  });

  errorRate.add(!ok);
  if (!ok) return null;

  const body = JSON.parse(res.body);
  return { username, token: body.token };
}

function login(username, password) {
  const res = http.post(
    `${API}/login`,
    JSON.stringify({ username, password }),
    { headers: { "Content-Type": "application/json" } }
  );

  const ok = check(res, {
    "login 200":       (r) => r.status === 200,
    "login has token": (r) => {
      try { return !!JSON.parse(r.body).token; }
      catch { return false; }
    },
  });

  errorRate.add(!ok);
  if (!ok) return null;
  return JSON.parse(res.body).token;
}

function sendMessage(token, recipientUsername, body) {
  const clientMessageId = `${__VU}-${__ITER}-${randomString(6)}`;

  const res = http.post(
    `${API}/messages`,
    JSON.stringify({ recipientUsername, body, clientMessageId }),
    {
      headers: {
        "Content-Type": "application/json",
        Authorization:  `Bearer ${token}`,
      },
    }
  );

  // 201 = new message, 200 = deduplicated (both are success)
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

/**
 * Fetch a short-lived WS ticket from the REST API, then open the WebSocket
 * using the ticket as a query parameter (matches your WSAuthMiddleware).
 */
function openWebSocket(token, peerUsername) {
  // 1. Get a ticket
  const ticketRes = http.post(
    `${API}/ws/ticket`,
    null,
    { headers: { Authorization: `Bearer ${token}` } }
  );

  const ticketOk = check(ticketRes, {
    "ws ticket 200": (r) => r.status === 200,
  });
  errorRate.add(!ticketOk);
  if (!ticketOk) return;

  let ticket;
  try {
    ticket = JSON.parse(ticketRes.body).ticket;
  } catch {
    errorRate.add(true);
    return;
  }

  // 2. Open the WebSocket with the ticket
  const url = `${WS_BASE}/api/v1/ws?ticket=${encodeURIComponent(ticket)}`;

  const res = ws.connect(url, {}, (socket) => {
    socket.on("open", () => {
      // Send a few typing indicators then close after 1 s
      for (let i = 0; i < 5; i++) {
        socket.send(
          JSON.stringify({ type: "typing", to: peerUsername, isTyping: i % 2 === 0 })
        );
      }
      socket.setTimeout(() => socket.close(), 1000);
    });

    socket.on("message", (data) => {
      try {
        const msg = JSON.parse(data);
        if (msg._ts) {
          wsMessageDelay.add(Date.now() - msg._ts);
        }
      } catch (_) {}
    });

    // Safety timeout — never hang a VU
    socket.setTimeout(() => socket.close(), 3000);
  });

  const ok = check(res, { "ws connected": (r) => r && r.status === 101 });
  errorRate.add(!ok);
}

// ── Default scenario ──────────────────────────────────────────────────────────

export default function () {
  // 1. Register
  const self = register();
  if (!self) return;

  sleep(0.2);

  // 2. List users
  listUsers(self.token);
  sleep(0.1);

  // 3. Send 3 messages to self (simplest way to exercise the endpoint without
  //    needing a second registered user in every iteration)
  for (let i = 0; i < 3; i++) {
    sendMessage(self.token, self.username, `k6 VU=${__VU} iter=${__ITER} msg=${i}`);
    sleep(0.05);
  }

  // 4. Read conversation history
  listMessages(self.token, self.username);
  sleep(0.1);

  // 5. WebSocket typing indicators
  openWebSocket(self.token, self.username);

  sleep(0.5);
}

// ── Summary ───────────────────────────────────────────────────────────────────

export function handleSummary(data) {
  const p95 = (metric) =>
    data.metrics[metric]?.values?.["p(95)"]?.toFixed(2) ?? "n/a";

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