# roomkit

**Self-hosted WebRTC video calls for any web app.** roomkit is a single Go
binary — a Pion SFU, an embedded TURN server, and ffmpeg-based recording — that
runs as one Docker container with no external services. Add real-time video to
your frontend with the vanilla JS, React, or Vue SDK and build the UI your way.

[![Release](https://img.shields.io/github/v/tag/dixydo/roomkit-core?sort=semver&label=release)](https://github.com/dixydo/roomkit-core/releases)
[![Build](https://img.shields.io/github/actions/workflow/status/dixydo/roomkit-core/ci.yml?branch=main&label=build)](https://github.com/dixydo/roomkit-core/actions/workflows/ci.yml)
[![License](https://img.shields.io/github/license/dixydo/roomkit-core)](./LICENSE)

[![npm @dixydo/roomkit](https://img.shields.io/npm/v/@dixydo/roomkit?label=@dixydo/roomkit)](https://www.npmjs.com/package/@dixydo/roomkit)
[![npm @dixydo/roomkit-react](https://img.shields.io/npm/v/@dixydo/roomkit-react?label=@dixydo/roomkit-react)](https://www.npmjs.com/package/@dixydo/roomkit-react)
[![npm @dixydo/roomkit-vue](https://img.shields.io/npm/v/@dixydo/roomkit-vue?label=@dixydo/roomkit-vue)](https://www.npmjs.com/package/@dixydo/roomkit-vue)

[**Deploy your own →**](./docs/DEPLOY.md)

---

## Quick start

### 1. Start a roomkit server locally

```bash
git clone https://github.com/dixydo/roomkit-core.git
cd roomkit-core
make run        # builds binary, loads .env, starts on http://localhost:8080
```

**Prefer a prebuilt binary?** Each release ships standalone binaries (no Go
toolchain needed) for Linux, macOS, and Windows on amd64/arm64. Grab one from
the [releases page](https://github.com/dixydo/roomkit-core/releases):

```bash
# example: Linux x86-64 — pick the asset matching your OS/arch
curl -fsSL https://github.com/dixydo/roomkit-core/releases/latest/download/roomkit_<version>_linux_amd64.tar.gz | tar xz
./roomkit       # starts on http://localhost:8080 (ffmpeg only needed for recording)
```

`make run` also creates `.env` from `.env.example` if it doesn't exist yet
(including a generated `ROOMKIT_ROOM_TOKEN_SECRET`). Recording is **off** by
default locally — set `ROOMKIT_REC_ENABLED=true` in `.env` and install ffmpeg
first if you want to test it:

```bash
# macOS
brew install ffmpeg

# Ubuntu / Debian
sudo apt-get install -y ffmpeg
```

### 2. See it run

roomkit has no built-in UI — it's a server plus client SDKs. The fastest way to
watch a call work end-to-end is the bundled vanilla demo:

```bash
cd sdk && npm install && npm run build
open examples/vanilla-demo.html      # then point it at http://localhost:8080
```

Open it in two browser tabs (or two devices), join the same room, and you have a
call. Use it as a copy-paste starting point for your own UI.

### 3. Add a video call to your site

Point any of these at `http://localhost:8080` (or your deployed URL).

#### Vanilla JS (no build step)

Load the UMD bundle straight from a CDN — it exposes a global `Roomkit`:

```html
<script src="https://unpkg.com/@dixydo/roomkit"></script>
<script type="module">
  const room = new Roomkit.RoomkitRoom({
    serverUrl: 'http://localhost:8080',
    roomId: 'hello-world',
  })
  room.on('trackPublished', ({ peerId, stream }) => {
    const v = document.createElement('video')
    v.autoplay = true; v.playsInline = true; v.srcObject = stream
    document.getElementById('remotes').appendChild(v)
  })
  await room.join()
</script>
```

#### Vanilla JS (with a bundler)

```bash
npm install @dixydo/roomkit
```

```ts
import { RoomkitRoom } from '@dixydo/roomkit'

const room = new RoomkitRoom({ serverUrl: 'http://localhost:8080', roomId: 'hello-world' })
room.on('trackPublished', ({ peerId, stream }) => { /* attach stream to a <video> */ })
await room.join()
```

#### React

```bash
npm install @dixydo/roomkit-react @dixydo/roomkit
```

```tsx
import { useRoomkit, RoomkitVideo } from '@dixydo/roomkit-react'

export function Call() {
  const { localStream, peers } = useRoomkit({
    serverUrl: 'https://meet.yourdomain.com',
    roomId: 'hello-world',
    autoJoin: true,
  })
  return (
    <>
      <RoomkitVideo stream={localStream} muted />
      {[...peers].map(([id, p]) => (
        <RoomkitVideo key={id} stream={p.stream} />
      ))}
    </>
  )
}
```

#### Vue 3

```bash
npm install @dixydo/roomkit-vue @dixydo/roomkit vue
```

```vue
<script setup>
import { useRoomkit, RoomkitVideo } from '@dixydo/roomkit-vue'
const { localStream, peers } = useRoomkit({
  serverUrl: 'https://meet.yourdomain.com',
  roomId: 'hello-world',
  autoJoin: true,
})
</script>
<template>
  <RoomkitVideo :stream="localStream" muted />
  <RoomkitVideo v-for="[id, p] in peers" :key="id" :stream="p.stream" />
</template>
```

---

## Deploy to a VPS

### One-click installer (Ubuntu 22.04+)

```bash
curl -fsSL https://raw.githubusercontent.com/dixydo/roomkit/main/install.sh | bash
```

Prompts for your domain, auto-detects your public IP, generates secrets, opens
the firewall, installs Docker, and starts the stack.

### Manual deploy

```bash
# On your VPS
curl -fsSL https://get.docker.com | sudo sh
git clone https://github.com/dixydo/roomkit-core ~/roomkit
cd ~/roomkit
make init-env     # creates .env with generated secret
nano .env         # set DOMAIN and ROOMKIT_PUBLIC_URL at minimum
make docker-compose-up
```

Full deploy guide with DNS, TLS, TURN, and optional S3 recording:
[docs/DEPLOY.md](./docs/DEPLOY.md).

---

## What's inside

- **SFU on Pion v4.** Each client uploads its tracks once; server fans them out.
  4+ participants don't melt laptops.
- **Embedded TURN.** Runs as a goroutine inside the same Go binary — most
  setups need no separate coturn.
- **Auto-TLS.** Bundled Caddy fetches Let's Encrypt certs on first hit.
- **Server-side recording.** Records each track as IVF/Ogg, muxes with ffmpeg
  into one MP4. Saves to local disk by default (`ROOMKIT_REC_ENABLED=true`) or
  uploads to any S3-compatible bucket.
- **No accounts by default.** Any client that connects joins in open mode.
  Optional HS256 room tokens when you need per-user join/record permissions.
- **One binary, one ~140 MB Docker image.** Ships with ffmpeg.

Architecture deep dive: [docs/ARCHITECTURE.md](./docs/ARCHITECTURE.md).

---

## Configuration

Minimum `.env` for local dev (auto-created by `make init-env`):

```env
ROOMKIT_REC_ENABLED=true          # enable recording (needs ffmpeg)
ROOMKIT_ROOM_TOKEN_SECRET=<hex>   # auto-generated; leave empty for open mode
```

Minimum `.env` for a VPS:

```env
DOMAIN=meet.yourdomain.com
ROOMKIT_PUBLIC_URL=https://meet.yourdomain.com
ROOMKIT_ALLOWED_ORIGINS=https://meet.yourdomain.com
ROOMKIT_ROOM_TOKEN_SECRET=<openssl rand -hex 32>
ROOMKIT_TURN_PUBLIC_IP=<your-vps-ip>
ROOMKIT_TURN_SECRET=<openssl rand -hex 32>
ROOMKIT_REC_ENABLED=true
```

Full configuration reference: [docs/DEPLOY.md](./docs/DEPLOY.md#configuration-reference).

---

## How does it work

```
                            ┌─────────────────────────────────┐
                            │            Browser              │
                            └───────────┬─────────────────────┘
                                        │
                   HTTPS + WSS          │   WebRTC (SRTP)
                        ┌───────────────┼──────────────────────┐
                        │               │                      │
                        ▼               ▼                      ▼
                  ┌──────────┐   ┌──────────────┐      ┌──────────────┐
                  │ Caddy 80 │   │  Go binary   │      │  Go binary   │
                  │   /443   │──▶│  HTTP+WS+SFU │      │ embedded TURN│
                  └──────────┘   │              │      │ (UDP 3478 +  │
                   (auto-TLS)    │   pion v4    │      │  ephemeral)  │
                                 └──────┬───────┘      └──────────────┘
                                        │
                                 RTP fan-out
                                        │
                                        ▼
                                 ┌──────────────┐
                                 │  Recording   │  IVF + Ogg per track
                                 │ ffmpeg → MP4 │──▶ local /recordings/
                                 │              │    OR s3://bucket/...
                                 └──────────────┘
```

## Tech stack

| Layer | Pick |
|---|---|
| Backend | Go 1.24, [pion/webrtc v4](https://github.com/pion/webrtc), [pion/turn v3](https://github.com/pion/turn), [coder/websocket](https://github.com/coder/websocket), [aws-sdk-go-v2](https://github.com/aws/aws-sdk-go-v2) |
| SDK | TypeScript, Vite (ESM + UMD + .d.ts) |
| Infra | Docker, Caddy 2, GitHub Actions, ffmpeg |

## Docs

| | |
|---|---|
| [**DEPLOY.md**](./docs/DEPLOY.md) | Production VPS deploy + TURN + S3 + auto-deploy |
| [**ARCHITECTURE.md**](./docs/ARCHITECTURE.md) | SFU design, signaling protocol, recording pipeline |

## FAQ

**How is this different from Jitsi / LiveKit / Daily?** Smaller in scope.
roomkit is for the case where you want a single Docker image you can read
end-to-end in a weekend (~3K Go + ~3K TypeScript), no signup, no per-minute
pricing. Jitsi / LiveKit are more featureful and more complex; Daily is hosted
and great but commercial.

**Can I use it commercially?** Yes — MIT licensed. Run it on your own
infrastructure for production traffic.

**What about group call limits?** Tested cleanly to 6 publishers per room.
Bottleneck at scale is server bandwidth (each publisher × every subscriber).

**Mobile apps?** Browser only — both iOS Safari and Chrome on Android work.

**Is there a built-in UI?** No. roomkit is a server + client SDKs, not a
finished meeting app — you build (or design) the call UI that fits your product.
The `sdk/examples/vanilla-demo.html` page is a ~150-line, dependency-free
starting point.

**Auth?** Open by default: any client that connects joins. For production, set
`ROOMKIT_ROOM_TOKEN_SECRET` and pass short-lived HS256 room tokens to the SDK.

**Recording storage?** By default (`ROOMKIT_REC_ENABLED=true`) recordings are
saved to disk and served at `/recordings/`. For permanent storage, configure
`ROOMKIT_S3_*` to upload to DigitalOcean Spaces, AWS S3, Cloudflare R2, or any
S3-compatible bucket.

**Why VP8 + Opus only?** The recording writers (Pion IVF + Ogg) are
codec-specific. Restricting the MediaEngine to VP8/Opus keeps recording
predictable across browsers. H264/VP9 + simulcast are on the roadmap.

## License

MIT — see [LICENSE](./LICENSE).

## Acknowledgments

Built on the shoulders of [Pion](https://github.com/pion) (WebRTC in Go),
[Caddy](https://caddyserver.com/) (the TLS-by-default reverse proxy).
