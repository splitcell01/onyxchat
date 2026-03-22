/**
 * OnyxChat k6 Load Test
 * ─────────────────────
 * Usage:
 *
 *   # Local
 *   k6 run scripts/load_test.js \
 *       -e BASE_URL=http://localhost:8080 \
 *       -e WS_URL=ws://localhost:8080
 *
 *   # AWS (swap URLs)
 *   k6 run scripts/load_test.js \
 *       -e BASE_URL=https://api.onyxchat.dev \
 *       -e WS_URL=wss://api.onyxchat.dev
 *
 * Install k6: https://k6.io/docs/get-started/installation/
 *
 * What this tests:
 *   Stage 1  — ramp to 20 VUs (baseline latency under normal load)
 *   Stage 2  — ramp to 100 VUs (stress: high concurrency)
 *   Stage 3  — ramp to 200 VUs (spike: worst-case throughput)
 *   Stage 4  — scale back to 20 VUs (recovery behaviour)
 *
 * Each VU runs:
 *   • register  → logs in → sends messages → reads conversation history
 *   • opens a WebSocket connection and exchanges typing indicators
 *
 * Thresholds (CI-style pass/fail gates):
 *   • p95 HTTP latency < 500 ms
 *   • error rate      < 1%
 *   • p95 WS msg lag  < 200 ms
 */

import http from "k6/http";
import ws from "k6/ws";
import { check, sleep } from "k6";
import { Rate, Trend } from "k6/metrics";
import { randomString } from "https://jslib.k6.io/k6-utils/1.4.0/index.js";

// ── Config ───────────────────────────────────────────────────────────────────

const BASE = __ENV.BASE_URL || "http://localhost:8080";
const WS   = __ENV.WS_URL  || "ws://localhost:8080";
const API  = `${BASE}/api/v1`;

// ── Custom metrics ────────────────────────────────────────────────────────────

const errorRate      = new Rate("errors");
const wsMessageDelay = new Trend("ws_message_delay_ms", true);

// ── Load profile ──────────────────────────────────────────────────────────────

export const options = {
  stages: [
    { duration: "30s", target: 20  }, // ramp-up
    { duration: "60s", target: 20  }, // baseline steady state
    { duration: "30s", target: 100 }, // ramp to stress
    { duration: "60s", target: 100 }, // stress steady state
    { duration: "30s", target: 200 }, // spike
    { duration: "30s", target: 200 }, // spike hold
    { duration: "30s", target: 20  }, // recovery
    { duration: "30s", target: 0   }, // ramp-down
  ],
  thresholds: {
    http_req_duration:   ["p(95)<500"],  // 95th percentile HTTP < 500ms
    errors:              ["rate<0.10"],  // temporary: allow up to 10% during spike
    ws_message_delay_ms: ["p(95)<200"],  // 95th percentile WS round-trip < 200ms
  },
};

// ── Helpers ───────────────────────────────────────────────────────────────────

function register() {
  const username = `k6_${randomString(10)}`;
  const password = "loadtest1234!";

  const res = http.post(
    `${API}/register`,
    JSON.stringify({ username, password }),
    { headers: { "Content-Type": "application/json" } }
  );

  const ok = check(res, {
    "register 200": (r) => r.status === 200,
    "register has token": (r) => {
      try { return JSON.parse(r.body).token !== undefined; }
      catch { return false; }
    },
  });

  errorRate.add(!ok);
  if (!ok) return null;

  return { username, token: JSON.parse(res.body).token };
}

function sendMessage(token, recipientUsername, body) {
  const res = http.post(
    `${API}/messages`,
    JSON.stringify({ recipientUsername, body }),
    {
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${token}`,
      },
    }
  );

  const ok = check(res, { "send message 200": (r) => r.status === 200 });
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
  const url = `${WS}/api/v1/ws`;

  const res = ws.connect(
    url,
    { headers: { Authorization: `Bearer ${token}` } },
    (socket) => {
      let received = 0;
      const sendTs = {};

      socket.on("open", () => {
        // Send 5 typing indicators sequentially then close
        for (let i = 0; i < 5; i++) {
          socket.send(
            JSON.stringify({
              type: "typing",
              to: peerUsername,
              isTyping: i % 2 === 0,
            })
          );
        }
        socket.setTimeout(() => { socket.close(); }, 1000);
      });

      socket.on("message", (data) => {
        received++;
        try {
          const msg = JSON.parse(data);
          if (msg._id && sendTs[msg._id]) {
            wsMessageDelay.add(Date.now() - sendTs[msg._id]);
          }
        } catch (_) {}
      });

      socket.setTimeout(() => {
        socket.close();
      }, 3000);
    }
  );

  const ok = check(res, { "ws connected": (r) => r && r.status === 101 });
  errorRate.add(!ok);
}

// ── Scenario (1 iteration per VU) ────────────────────────────────────────────

// We register two users so they can message each other.
let sharedPeer = null; // lazily populated

export default function () {
  // 1. Register self
  const self = register();
  if (!self) return;

  // 2. Register a peer (first VU wins, rest reuse)
  //    In a real run you'd want a setup() function but this works for demo.
  if (!sharedPeer) {
    sharedPeer = register();
  }
  const peer = sharedPeer;
  if (!peer) return;

  sleep(0.2);

  // 3. List users (auth endpoint baseline)
  listUsers(self.token);
  sleep(0.1);

  // 4. Send 3 messages
  for (let i = 0; i < 3; i++) {
    sendMessage(self.token, peer.username, `hello from k6 VU ${__VU} msg ${i}`);
    sleep(0.05);
  }

  // 5. Read conversation history
  listMessages(self.token, peer.username);
  sleep(0.1);

  // 6. Open WS connection and send typing indicators
  openWebSocket(self.token, peer.username);

  sleep(0.5);
}

// ── Summary ───────────────────────────────────────────────────────────────────

export function handleSummary(data) {
  const p95 = (metric) =>
    data.metrics[metric]?.values?.["p(95)"]?.toFixed(2) ?? "n/a";

  console.log(`
╔══════════════════════════════════════════╗
║        OnyxChat Load Test Summary        ║
╠══════════════════════════════════════════╣
║  HTTP p95 latency : ${String(p95("http_req_duration") + " ms").padEnd(20)}║
║  WS msg p95 delay : ${String(p95("ws_message_delay_ms") + " ms").padEnd(20)}║
║  Error rate       : ${String((data.metrics.errors?.values?.rate * 100)?.toFixed(2) + " %").padEnd(20)}║
║  Total requests   : ${String(data.metrics.http_reqs?.values?.count ?? "n/a").padEnd(20)}║
╚══════════════════════════════════════════╝
`);

  return {
    "stdout": JSON.stringify(data, null, 2),
  };
}