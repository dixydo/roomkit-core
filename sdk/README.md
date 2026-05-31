# @dixydo/roomkit

[![npm](https://img.shields.io/npm/v/@dixydo/roomkit?label=npm)](https://www.npmjs.com/package/@dixydo/roomkit)
[![bundle size](https://img.shields.io/bundlephobia/minzip/@dixydo/roomkit)](https://bundlephobia.com/package/@dixydo/roomkit)
[![License](https://img.shields.io/npm/l/@dixydo/roomkit)](https://github.com/dixydo/roomkit/blob/main/LICENSE)

JavaScript SDK for embedding [roomkit](https://github.com/dixydo/roomkit)
video calls into any web page.

Mirrors the developer ergonomics of 100ms and Agora: construct a room, wire
events, call methods. Works in vanilla JS, React, Vue, Svelte — no framework
assumed.

## Which package?

| Package | Use when |
|---|---|
| **`@dixydo/roomkit`** (this package) | Vanilla JS, or any framework not listed below |
| **[`@dixydo/roomkit-react`](../sdk-react/)** | React app (hook + `<RoomkitVideo>`) |
| **[`@dixydo/roomkit-vue`](../sdk-vue/)** | Vue 3 app (composable + components) |

> roomkit has no built-in UI — these packages give you state + media plumbing,
> you render the call however you like. See
> [`examples/vanilla-demo.html`](examples/vanilla-demo.html) for a complete,
> dependency-free reference UI.

## Install

### As an ES module via bundler

```bash
npm install @dixydo/roomkit
```

```ts
import { RoomkitRoom } from '@dixydo/roomkit'
```

### From a `<script>` tag (UMD, no build step)

The published package is available on any npm CDN. It exposes a global `Roomkit`:

```html
<script src="https://unpkg.com/@dixydo/roomkit"></script>
<script>
  const room = new Roomkit.RoomkitRoom({
    serverUrl: 'https://meet.example.com',
    roomId: 'team-standup',
  })
  // ...
</script>
```

## Quick start

```ts
const room = new RoomkitRoom({
  serverUrl: 'https://meet.example.com',
  roomId: 'team-standup',
})

room.on('joined', ({ peerId }) => console.log('I am', peerId))
room.on('trackPublished', ({ peerId, stream }) => {
  // Attach to a <video> element
  const video = document.createElement('video')
  video.srcObject = stream
  video.autoplay = true
  video.playsInline = true
  document.body.appendChild(video)
})
room.on('peerLeft', ({ peerId }) => {
  document.querySelector(`#peer-${peerId}`)?.remove()
})

await room.join({ audio: true, video: true })

// Local preview
const meEl = document.querySelector('#me')
room.attachLocalVideo(meEl)

// Controls
room.setMicEnabled(false)
room.setCamEnabled(false)
await room.startScreenShare()

// Chat
room.sendChat('hello team')

// Recording (only works when the roomkit server has recording enabled)
room.startRecording()
room.stopRecording()

// Cleanup
await room.leave()
```

## API reference

### `new RoomkitRoom(options)`

| option | type | required | notes |
| --- | --- | --- | --- |
| `serverUrl` | string | yes | base URL of roomkit server |
| `roomId` | string | yes | room identifier |
| `token` | string | no | short-lived HS256 room token; required when the server has `ROOMKIT_ROOM_TOKEN_SECRET` set |
| `peerId` | string | no | provide your own id; otherwise server assigns UUID |

### Methods

- `join(opts?)` — request media + connect WS + publish tracks
- `leave()` — stop all tracks + close connections
- `setMicEnabled(b)`, `setCamEnabled(b)`
- `startScreenShare()`, `stopScreenShare()`, `toggleScreenShare()`
- `setCameraDevice(deviceId)`, `setMicrophoneDevice(deviceId)`
- `listDevices()` — `{ cameras, microphones }`
- `sendChat(text)`
- `startRecording()`, `stopRecording()`
- `attachLocalVideo(el)`, `attachRemoteVideo(peerId, el)`

### State accessors

- `localPeerId`, `localStream`, `peers`, `chatMessages`, `recording`
- `micEnabled`, `cameraEnabled`, `screenSharing`

### Events (`room.on(name, handler)`)

- `joined({ peerId })`, `left`
- `peerJoined({ peerId })`, `peerLeft({ peerId })`
- `trackPublished({ peerId, stream })`, `trackUnpublished({ peerId })`
- `chatMessage(msg)` — for both incoming and self messages
- `recordingStateChange(status)` — `state` is one of
  `idle | recording | processing | ready | failed`
- `error(err)`

`room.on()` returns an unsubscribe function.

## Server requirements

For cross-origin embeds the roomkit server must:

1. Allow your origin via env: `ROOMKIT_ALLOWED_ORIGINS=https://yoursite.com`
   (controls both the WS Origin check AND CORS on `/api/*`)
2. Have TURN configured if any of your users are behind symmetric NAT
3. If `ROOMKIT_ROOM_TOKEN_SECRET` is set, pass `token` when constructing the
   room. The token must be an HS256 JWT with `room_id`, `exp`, and a `join`
   permission.

## Local development

```bash
cd sdk
npm install
npm run build       # produces dist/roomkit-sdk.js (ESM) + .umd.cjs + .d.ts
npm run dev         # rebuild on save
```
