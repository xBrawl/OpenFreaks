# Browser transport (Go)

Live endpoints:
- `wss://serbiansubtitles.online/freaks/ws/main`, subprotocol `openfreaks.main.v1`.
- `wss://serbiansubtitles.online/freaks/ws/battle`, subprotocol `openfreaks.battle.v1`.

Use binary WebSocket messages (`binaryType = "arraybuffer"`). TLS terminates at nginx. The Go listeners are loopback-only,9729 and9730. `/allocate`, `/result` and the other internal HTTP endpoints are not exposed through these paths. `denis.kornilov12.fvds.ru` does not match the installed TLS certificate; use the WSS hostname above.

## Main

Each binary message contains exactly one normal Freaks TCP frame: `[messageId:BE16][payloadLength:BE32][payload]`. The payload retains the existing ByteStream/variable-integer format. Do not serialize payload fields as fixed-width integers. No TCP frame may be split across WebSocket messages; WebSocket's own fragmentation is supported by the library. Incoming message limit64KiB; outgoing normal TCP frame limit32MiB. Text messages and malformed frames close the connection.

Run the normal Hello → fingerprint/models → Login flow. Normal application KeepAlive messages remain required. Authentication, profile persistence, match allocation and account connection limits are the same handlers as TCP.

## Battle

1. Obtain SessionID from the authenticated main's BattleStarted message (or replay grant).
2. Open the battle URL with subprotocol `openfreaks.battle.v1`.
3. Within5seconds send exactly8binary bytes: SessionID as positive signed64-bit **big endian**. Keep it as BigInt/string/8bytes in JavaScript; Number cannot safely represent it. Never put it in a URL, query string or log.
4. Wait for a one-byte binary acknowledgment `[0x01]`.
5. Send raw existing battle application frames (`EncodeBattleClientFrame`, PlayerActions ID2). No UDP session envelope, KCP segmentation, kcp2k data byte, or UDP Hello is needed.
6. Receive raw existing `EncodeBattleServerFrame` frames. Feed them to the existing battle decoder. Browser handles WebSocket Ping/Pong automatically.

Both UDP and WebSocket players share the same Match/World and validation. Session replacement closes the old connection; explicit leave revokes the grant. Replays use the same session mechanism. Ordered control queue:64frames. Full snapshots keep only the newest pending state. A stalled write or ping times out after5seconds. WebSocket uses TCP: dropping queued snapshots reduces backlog but cannot remove TCP head-of-line blocking.

## Origins and deployment

`Main.Network.webSocketBind` and `Battle.Network.webSocketBind` enable listeners; empty disables them. `webSocketOrigins` permits additional explicit browser origins; same-host origins are accepted by default. Live config additionally allows `http://localhost:8000` and `http://127.0.0.1:8000`. Change the list when hosting the web client on another domain. Unsupported subprotocols, foreign origins and text payloads are rejected. Connection admission is bounded, including unauthenticated sockets.

Nginx include: `deploy/nginx-websocket.conf`. Docker publishes WS ports to127.0.0.1 by default (`WEB_BIND_HOST`, `MAIN_WS_PORT`, `BATTLE_WS_PORT`). Put HTTPS/WSS reverse proxy in front of them for internet use.

## Verified

Local full flow (register→match→battle snapshots), rate/backpressure limits, foreign Origin rejection, pre-login rejection, reconnect and grant revocation, and full `go test -race ./...`. Live WSS full flow passed with browser-style Origin.20 simultaneous WSS clients completed registration, matchmaking and battle snapshot reception on VDS in23.37seconds. This is a connection smoke test, not a prolonged soak test.

Unity WebGL transport/JavaScript bridge is a separate client task and has not been wired into the Unity client here. The current native Unity TCP/UDP transport remains available.
