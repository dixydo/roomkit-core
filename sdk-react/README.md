# @dixydo/roomkit-react

[![npm](https://img.shields.io/npm/v/@dixydo/roomkit-react?label=npm)](https://www.npmjs.com/package/@dixydo/roomkit-react)
[![License](https://img.shields.io/npm/l/@dixydo/roomkit-react)](https://github.com/dixydo/roomkit/blob/main/LICENSE)

React bindings for [`@dixydo/roomkit`](https://www.npmjs.com/package/@dixydo/roomkit).

## Install

```bash
npm install @dixydo/roomkit-react @dixydo/roomkit
```

## Usage

```tsx
import { useRoomkit, RoomkitVideo } from '@dixydo/roomkit-react'

function VideoCall() {
  const {
    status, error,
    localStream, peers,
    micEnabled, cameraEnabled, screenSharing, recording,
    setMicEnabled, setCamEnabled, toggleScreenShare,
    startRecording, stopRecording, leave,
  } = useRoomkit({
    serverUrl: 'https://meet.example.com',
    roomId: 'team-standup',
    autoJoin: true,
  })

  if (status === 'error') return <div>Error: {error?.message}</div>
  if (status !== 'joined') return <div>Joining…</div>

  return (
    <div>
      <RoomkitVideo stream={localStream} muted />
      {[...peers].map(([id, p]) => (
        <RoomkitVideo key={id} stream={p.stream} />
      ))}
      <button onClick={() => setMicEnabled(!micEnabled)}>
        {micEnabled ? 'Mute' : 'Unmute'}
      </button>
      <button onClick={() => setCamEnabled(!cameraEnabled)}>
        {cameraEnabled ? 'Stop cam' : 'Start cam'}
      </button>
      <button onClick={toggleScreenShare}>
        {screenSharing ? 'Stop sharing' : 'Share screen'}
      </button>
      {recording.state === 'recording'
        ? <button onClick={stopRecording}>Stop recording</button>
        : <button onClick={startRecording}>Record</button>}
      {recording.state === 'ready' && (
        <a href={recording.url} target="_blank" rel="noopener">Recording ready ↗</a>
      )}
      <button onClick={leave}>Leave</button>
    </div>
  )
}
```

## API

### `useRoomkit(options)` → `UseRoomkitApi`

`options`:
- `serverUrl: string` (required)
- `roomId: string` (required)
- `peerId?: string`
- `joinOptions?: { audio?, video? }`
- `autoJoin?: boolean` — default `true`. If `false`, call `api.join()` manually.

Returns an object with reactive state and bound action functions. See
[useRoomkit.ts](./src/useRoomkit.ts) for the full type.

**State:** `status`, `error`, `localPeerId`, `localStream`, `peers`,
`chatMessages`, `recording`, `micEnabled`, `cameraEnabled`, `screenSharing`.

**Actions:** `join`, `leave`, `setMicEnabled`, `setCamEnabled`,
`toggleScreenShare`, `setCameraDevice`, `setMicrophoneDevice`, `sendChat`,
`startRecording`, `stopRecording`.

### `<RoomkitVideo stream={…} muted? />`

Tiny wrapper around `<video>` that handles `srcObject` assignment.
