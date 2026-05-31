# Architecture

This doc explains how roomkit moves bytes between participants, why it's an
SFU instead of mesh, where TURN sits, and how recording works.

If you only need to deploy, you can skip this — see [DEPLOY.md](DEPLOY.md) instead.

## The big picture

```
                ┌────────────────────────────────────────────────┐
                │              Client (browser)                  │
                │  ┌──────────────┐         ┌──────────────────┐ │
                │  │ getUserMedia │         │  your UI +       │ │
                │  │  (mic + cam) │         │  roomkit SDK     │ │
                │  └──────┬───────┘         └────────┬─────────┘ │
                │         │                          │           │
                │  ┌──────▼──────────────────────────▼────────┐  │
                │  │   2 × RTCPeerConnection                   │  │
                │  │   ┌──────────────┐  ┌──────────────┐     │  │
                │  │   │ publisher PC │  │subscriber PC │     │  │
                │  │   │  sendonly    │  │  recvonly    │     │  │
                │  │   └──────┬───────┘  └──────┬───────┘     │  │
                │  └──────────┼──────────────────┼────────────┘  │
                └─────────────┼──────────────────┼───────────────┘
                              │                  │
                              │  WSS signaling   │
                              ▼   (offer/answer/ice/chat/record-*)
                ┌─────────────────────────────────────────────────┐
                │              Server (Go binary)                  │
                │  ┌────────────┐  ┌──────────────────────────┐   │
                │  │ Caddy 80/  │  │  HTTP/1.1 + WebSocket    │   │
                │  │   443      │─▶│ /ws /api/* /healthz /rec… │   │
                │  └────────────┘  └─────┬────────────────────┘   │
                │                        │                        │
                │   ┌────────────────────▼─────────────────────┐  │
                │   │   internal/signaling — Hub + Client      │  │
                │   └─────┬─────────────────────┬──────────────┘  │
                │         │                     │                 │
                │   ┌─────▼─────┐         ┌─────▼─────┐           │
                │   │internal/  │         │internal/  │           │
                │   │   sfu     │◀──────▶ │ recording │           │
                │   │ Pion v4   │ RTP tee │ IVF + Ogg │           │
                │   └─────┬─────┘         │  → ffmpeg │           │
                │         │               │  → S3     │           │
                │         │               └───────────┘           │
                │   ┌─────▼─────┐                                 │
                │   │internal/  │                                 │
                │   │   turn    │  UDP 3478 + ephemeral 49152-…   │
                │   │ Pion TURN │                                 │
                │   └───────────┘                                 │
                └──────────────────────────────────────────────────┘
```

## Why SFU, not mesh

In a mesh topology each client opens N-1 PeerConnections to every other
client. Encoders multiply, upload bandwidth multiplies, CPU multiplies.

For N participants at 720p/1.5 Mbps:

| N | Upload per client | Encoders per client | Mesh? |
|---|---|---|---|
| 2 | 1.5 Mbps  | 1  | fine |
| 3 | 3.0 Mbps  | 2  | fine |
| 4 | 4.5 Mbps  | 3  | tight |
| 5 | 6.0 Mbps  | 4  | breaks on home wifi |
| 10 | 13.5 Mbps | 9  | impossible |

An SFU (Selective Forwarding Unit) inverts the topology: every client opens
exactly two PeerConnections to the *server*. Upload becomes constant
(1.5 Mbps) regardless of room size. CPU stays at one encoder + N-1 decoders.

The server pays the bandwidth tax (now N×(N-1) streams flow through the
VPS), but a VPS has gigabit links — much better than the slowest home
upload in the room dragging everyone down.

## Two PeerConnections per client

```
client
 │
 ├─ publisher PC ─────────────▶  server (only sends, server only receives)
 │                                 │
 │                                 ▼
 │                          [RTP forwarding loop]
 │                                 │
 │                                 ▼
 └─ subscriber PC ◀────────────  server (only receives, server only sends)
                                  fans out every other peer's tracks
```

Why two PCs? Separation of concerns. The publisher PC only renegotiates
when *you* add/remove tracks (e.g. screen share replacing camera via
`replaceTrack` doesn't even renegotiate). The subscriber PC only
renegotiates when *others* publish or unpublish.

Mixing both directions on one PC works but adds glare risk (both sides
trying to renegotiate at once). Pion's own SFU example uses two PCs for
the same reason.

## Signaling protocol

WebSocket at `/ws?room=<id>&peer=<uuid>`. All messages are JSON, single
envelope:

```ts
type Message = {
  type: string
  from?: string         // server fills this on read
  to?: string           // not used in current SFU mode
  peerId?: string       // welcome / peer-joined / peer-left
  peers?: string[]      // peers-list
  sdp?: string          // offer/answer payloads
  candidate?: ICECandidateInit
  role?: 'publisher' | 'subscriber'   // tags ICE candidates
  text?: string         // chat
  ts?: number           // chat + recording
  recordState?: 'idle' | 'recording' | 'processing' | 'ready' | 'failed'
  recordStartedBy?: string
  recordStartedAt?: number
  recordUrl?: string
  recordError?: string
}
```

### Lifecycle

```
1. Client opens WS                                            CLIENT → SERVER
2. Server sends "welcome" with assigned peerId                SERVER → client
3. Server broadcasts "peer-joined" to existing peers          SERVER → others
4. Server sends "peers-list" with already-present peer IDs    SERVER → client
```

### SFU negotiation

```
Publisher path (client initiates):
  1. client createOffer() with own tracks attached
  2. → "pub-offer" { sdp }
  3. ← "pub-answer" { sdp }
  4. ICE candidates trickle via "ice" {role: 'publisher', candidate}

Subscriber path (server initiates whenever room tracks change):
  5. ← "sub-offer" { sdp }   (fresh full SDP including all current tracks)
  6. client createAnswer()
  7. → "sub-answer" { sdp }
  8. ICE candidates via "ice" {role: 'subscriber', candidate}
```

The server lazily creates a subscriber PC for a client only when there's at
least one track to forward to them. Empty rooms don't trigger any subscriber
offer.

### Chat

Single-shot broadcast via the hub, no acks:

```
client → server: { type: 'chat', text: 'hello' }
server (Hub.Broadcast except sender) → others: { type: 'chat', from, text, ts }
```

### Recording

```
client → server: { type: 'record-start' }
server (Hub.BroadcastStatus everyone): { type: 'record-status', recordState: 'recording', recordStartedBy, recordStartedAt }

  ... while recording, server tees every published RTP packet into per-track writers ...

client → server: { type: 'record-stop' }
server: closes writers, runs ffmpeg + S3 upload in a goroutine
server → everyone: { type: 'record-status', recordState: 'processing' }
... later ...
server → everyone: { type: 'record-status', recordState: 'ready', recordUrl: 'https://...' }
```

`recordUrl` is either a presigned S3 URL (S3 mode) or a `/recordings/...` path
on the roomkit server itself (local mode).

## SFU implementation details

### Codec policy

`internal/sfu/manager.go` registers a *minimal* MediaEngine with only VP8 +
Opus. Browsers can offer richer codecs (VP9, H264, AV1) but the SDP
negotiation will trim down to VP8/Opus.

Why restrict? Recording writers (`ivfwriter` for VP8, `oggwriter` for Opus)
are codec-specific. Allowing arbitrary browser choice means we'd need a
writer per codec, plus probably software transcoding when codecs don't
match across peers.

### RTP forwarding

`internal/sfu/peer.go::handleIncomingTrack` is the hot path:

```go
go func() {
    buf := make([]byte, 1500)
    for {
        n, _, err := remote.Read(buf)         // read from publisher
        if err != nil { return }
        if _, err := local.Write(buf[:n]); err != nil { return }  // forward to subscribers
        if cb != nil {                         // recording active?
            var pkt rtp.Packet
            if err := pkt.Unmarshal(buf[:n]); err == nil {
                cb(roomID, peerID, trackID, kind, codec, &pkt)
            }
        }
    }
}()
```

One goroutine per (peer, kind) — so a 5-person room with audio+video each
has 10 forwarding goroutines, plus a `pliLoop` per video track requesting
keyframes every 3 seconds.

The `local.Write` is *not* a parse — it's raw RTP byte copy. The RTP
parse happens only when recording is on, and only for tee'ing.

### Renegotiation race protection

When multiple peers publish near-simultaneously, the server needs to add
tracks to existing subscribers and renegotiate them. If two renegotiations
race on the same subscriber, browser SDP state machines get confused.

`internal/sfu/peer.go` serializes subscriber operations with a single
`subMu` mutex. `AddSubscriberTrack`, `RemoveSubscriberTracksFrom`, and
`renegotiateSubscriber` all take it. If a previous subscriber offer is still
waiting for a browser answer, the peer records that another offer is needed
and sends it only after `sub-answer` returns the subscriber PC to `stable`.

## TURN

`internal/turn/turn.go` boots a Pion TURN server as a goroutine *inside the
same binary*. Uses the TURN REST API auth pattern:

```
username = "<unix-expiry>:roomkit"
password = base64(HMAC-SHA1(secret, username))
```

The HTTP endpoint `/api/ice-config` mints a fresh username/password pair
valid for 5 minutes and returns it embedded in `RTCIceServer[]`. The browser
uses it; the TURN server validates incoming auth with the same HMAC.

Why embedded TURN? Most personal-scale deployments only need a single
server. Bundling avoids the operational overhead of running coturn
separately. For multi-region or high-bandwidth setups, run a real coturn
fleet and point `ROOMKIT_TURN_PUBLIC_IP` at a virtual IP.

## Recording

```
[user clicks Record]
  → record-start message
  → recording.Manager.Start(roomID)
    → creates Session with per-(peer, track) lazy writers
    → broadcasts record-status: recording

[during the call]
  Every RTP packet that flows through the SFU is parsed and tee'd:
  → recording.Manager.OnPacket(roomID, peerID, trackID, kind, codec, *rtp.Packet)
    → session.WriteRTP(...)
      → on first packet for a (peer, track) pair, opens an IVF or Ogg writer

[user clicks Stop, or 8h timeout]
  → record-stop message
  → recording.Manager.Stop(roomID)
    → closes all writers
    → broadcasts record-status: processing
    → in a goroutine:
      1. ffmpeg -filter_complex with hstack + amix → single MP4
      2. S3 multipart upload (DigitalOcean Spaces compatible)
      3. broadcasts record-status: ready, recordUrl: ...
    → cleans up local dir
```

### ffmpeg muxing

`internal/recording/ffmpeg.go` builds a `filter_complex` graph:

```
[0:v]scale=640:360,setsar=1[v0];
[1:v]scale=640:360,setsar=1[v1];
[v0][v1]hstack=inputs=2[vout];
[2:a][3:a]amix=inputs=2:duration=longest:dropout_transition=0[aout]
```

So N publishers → N×640 pixel-wide horizontal strip + mixed audio. Late
joiners are aligned with `-itsoffset` based on the recorded start offset
of their first packet.

This is a v1 layout. 2×2 grid for ≤4, 3×3 for ≤9, active-speaker focus
mode — all on the roadmap.

### Storage

`internal/recording/s3.go` uses `aws-sdk-go-v2` with a custom `BaseEndpoint`
so the same code targets DigitalOcean Spaces, Cloudflare R2, Wasabi, MinIO,
or actual S3. Multipart uploads with 16 MiB parts for large files.

When S3 is not configured and `ROOMKIT_REC_ENABLED=true`, the muxed MP4 stays
in `WorkDir` and is served at `/recordings/<roomID>/<sessionID>/output.mp4` by
a static file handler in `internal/server/server.go`. Local files are *not*
cleaned up after muxing so they remain available for download.

## What's not here yet

- **Simulcast** — single resolution per peer, server picks one for all.
- **End-to-end encryption** — server sees the media. Insertable Streams is
  the path forward.
- **Active speaker / dominant speaker detection** on the server.
- **Bandwidth adaptation** beyond what browsers do client-side.
- **Multi-region cascade** — single SFU only.

See [CHANGELOG.md](../CHANGELOG.md) for what landed in each release.
