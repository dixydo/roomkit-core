<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRoomkit, type UseRoomkitOptions } from './useRoomkit'
import RkTile from './RkTile.vue'

const props = defineProps<UseRoomkitOptions>()
const emit = defineEmits<{ leave: [] }>()

const {
  status, error,
  localStream, peers,
  micEnabled, cameraEnabled, screenSharing, recording,
  micDeviceId, cameraDeviceId,
  setMicEnabled, setCamEnabled, toggleScreenShare,
  startRecording, stopRecording,
  setCameraDevice, setMicrophoneDevice, listDevices,
  leave,
} = useRoomkit(props)

async function doLeave() {
  await leave()
  emit('leave')
}

const mics = ref<{ deviceId: string; label: string }[]>([])
const cameras = ref<{ deviceId: string; label: string }[]>([])

watch(status, async (s) => {
  if (s !== 'joined') return
  const d = await listDevices()
  mics.value = d.microphones
  cameras.value = d.cameras
})

function onMicChange(e: Event) {
  setMicrophoneDevice((e.target as HTMLSelectElement).value)
}
function onCamChange(e: Event) {
  setCameraDevice((e.target as HTMLSelectElement).value)
}
</script>

<template>
  <p v-if="status === 'error'" class="rk-error">
    {{ error?.message ?? 'Connection failed' }}
  </p>

  <div v-if="status === 'joining'" class="rk-row">
    <span class="rk-spinner" />
    <span class="rk-status">Connecting…</span>
  </div>

  <div v-else class="rk-row">
    <button
      :class="micEnabled ? '' : 'rk-danger'"
      :disabled="status !== 'joined'"
      @click="setMicEnabled(!micEnabled)"
    >{{ micEnabled ? 'Mute mic' : 'Unmute mic' }}</button>

    <button
      :class="cameraEnabled ? '' : 'rk-danger'"
      :disabled="status !== 'joined'"
      @click="setCamEnabled(!cameraEnabled)"
    >{{ cameraEnabled ? 'Camera off' : 'Camera on' }}</button>

    <button
      :class="screenSharing ? 'rk-active' : ''"
      :disabled="status !== 'joined'"
      @click="toggleScreenShare"
    >{{ screenSharing ? 'Stop sharing' : 'Share screen' }}</button>

    <button
      v-if="recording.state === 'recording'"
      class="rk-danger"
      @click="stopRecording"
    >Stop recording</button>
    <button
      v-else
      :disabled="status !== 'joined' || recording.state === 'processing'"
      @click="startRecording"
    >
      <span v-if="recording.state === 'processing'" class="rk-spinner rk-spinner--sm" />
      {{ recording.state === 'processing' ? 'Processing…' : 'Record' }}
    </button>

    <a
      v-if="recording.state === 'ready'"
      :href="recording.url"
      target="_blank"
      rel="noopener"
    >Recording ready ↗</a>

    <button @click="doLeave">Leave</button>
    <span class="rk-status">status: {{ status }} · peers: {{ peers.size }}</span>
  </div>

  <div v-if="status === 'joined'" class="rk-row rk-devices">
    <label class="rk-status">Mic</label>
    <select :value="micDeviceId" @change="onMicChange">
      <option v-for="m in mics" :key="m.deviceId" :value="m.deviceId">{{ m.label }}</option>
    </select>
    <label class="rk-status">Camera</label>
    <select :value="cameraDeviceId" @change="onCamChange">
      <option v-for="c in cameras" :key="c.deviceId" :value="c.deviceId">{{ c.label }}</option>
    </select>
  </div>

  <div class="rk-tiles">
    <RkTile :stream="localStream" label="You" :muted="true" />
    <RkTile
      v-for="[id, p] in peers"
      :key="id"
      :stream="p.stream"
      :label="`Peer ${id.slice(0, 6)}`"
    />
  </div>
</template>

<style scoped>
.rk-error {
  color: #f87171; background: #f8717115;
  border: 1px solid #f8717130; border-radius: 6px;
  padding: 8px 12px; margin-bottom: 12px; font-size: 14px;
}

.rk-row {
  display: flex; gap: 8px; margin-bottom: 16px;
  flex-wrap: wrap; align-items: center;
}

.rk-row button, .rk-row a {
  padding: 8px 12px; font: inherit; cursor: pointer;
  background: #27272a; color: #fafafa;
  border: 1px solid #3f3f46; border-radius: 6px;
  text-decoration: none; display: inline-flex;
  align-items: center; gap: 6px;
}
.rk-row button:disabled { opacity: 0.5; cursor: default; }
.rk-row button.rk-active { background: #4f46e5; border-color: #6366f1; }
.rk-row button.rk-danger { background: #dc2626; border-color: #ef4444; }

.rk-devices select {
  padding: 6px 8px; font: inherit;
  background: #27272a; color: #fafafa;
  border: 1px solid #3f3f46; border-radius: 6px;
  max-width: 220px;
}

.rk-status { color: #a1a1aa; font-size: 12px; }

.rk-spinner {
  display: inline-block;
  width: 16px; height: 16px; flex-shrink: 0;
  border: 2px solid #3f3f46;
  border-top-color: #a1a1aa;
  border-radius: 50%;
  animation: rk-spin 0.7s linear infinite;
}
.rk-spinner--sm { width: 12px; height: 12px; border-width: 1.5px; }
@keyframes rk-spin { to { transform: rotate(360deg); } }

.rk-tiles {
  display: grid; gap: 8px;
  grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
}
</style>
