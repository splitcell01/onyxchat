const WebSocket = require("ws");

const url = process.argv[2];      // e.g. wss://api.onyxchat.dev/api/v1/ws
const token = process.argv[3];    // JWT
const toUser = process.argv[4] || "smoke_1765999966";

if (!url || !token) {
  console.error('usage: node ws_spam.js "wss://.../api/v1/ws" "JWT" recipient_username');
  process.exit(1);
}

const ws = new WebSocket(url, {
  headers: { Authorization: `Bearer ${token}` },
});

ws.on("open", () => {
  console.log("open");
  let i = 0;
  const t = setInterval(() => {
    i++;
    ws.send(JSON.stringify({ type: "typing", to: toUser, isTyping: (i % 2) === 0 }));
  }, 1);

  ws.on("close", (code, reason) => {
    console.log("close", code, reason.toString());
    clearInterval(t);
    process.exit(0);
  });
});

ws.on("error", (e) => console.error("error", e));
