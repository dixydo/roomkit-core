# @dixydo/roomkit-vue

[![npm](https://img.shields.io/npm/v/@dixydo/roomkit-vue?label=npm)](https://www.npmjs.com/package/@dixydo/roomkit-vue)
[![License](https://img.shields.io/npm/l/@dixydo/roomkit-vue)](https://github.com/dixydo/roomkit/blob/main/LICENSE)

Vue 3 bindings for [`@dixydo/roomkit`](https://www.npmjs.com/package/@dixydo/roomkit).

## Install

```bash
npm install @dixydo/roomkit-vue @dixydo/roomkit vue
```

## Usage

```vue
<script setup lang="ts">
import { useRoomkit, RoomkitVideo } from '@dixydo/roomkit-vue'

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
</script>

<template>
  <div v-if="status === 'error'">Error: {{ error?.message }}</div>
  <div v-else-if="status !== 'joined'">Joining…</div>

  <div v-else>
    <RoomkitVideo :stream="localStream" muted />
    <RoomkitVideo v-for="[id, p] in peers" :key="id" :stream="p.stream" />

    <button @click="setMicEnabled(!micEnabled)">
      {{ micEnabled ? 'Mute' : 'Unmute' }}
    </button>
    <button @click="setCamEnabled(!cameraEnabled)">
      {{ cameraEnabled ? 'Stop cam' : 'Start cam' }}
    </button>
    <button @click="toggleScreenShare">
      {{ screenSharing ? 'Stop sharing' : 'Share screen' }}
    </button>

    <template v-if="recording.state === 'recording'">
      <button @click="stopRecording">Stop recording</button>
    </template>
    <template v-else>
      <button @click="startRecording">Record</button>
    </template>
    <a v-if="recording.state === 'ready'" :href="recording.url" target="_blank">
      Recording ready ↗
    </a>

    <button @click="leave">Leave</button>
  </div>
</template>
```

## API

### `useRoomkit(options)` → `UseRoomkitApi`

Same shape as the React hook but all state values are `Ref`s and `peers`
is a reactive `Map`.

### `<RoomkitVideo :stream="..." :muted="false" />`

`<video>` wrapper that handles `srcObject` assignment reactively.
