<script setup lang="ts">
import { computed } from 'vue'
import RoomkitVideo from './RoomkitVideo.vue'
import { useAudioLevel } from './useAudioLevel'

const props = defineProps<{ stream: MediaStream | null; label: string; muted?: boolean }>()
const streamRef = computed(() => props.stream)
const { isSpeaking } = useAudioLevel(streamRef)
</script>

<template>
  <div class="rk-tile" :class="{ 'rk-tile--speaking': isSpeaking && !muted }">
    <RoomkitVideo :stream="stream" :muted="muted" />
    <div class="rk-label">{{ label }}</div>
  </div>
</template>

<style scoped>
.rk-tile {
  position: relative; background: #000; border-radius: 8px;
  overflow: hidden; aspect-ratio: 16/9;
  box-shadow: 0 0 0 2px transparent;
  transition: box-shadow 0.1s;
}
.rk-tile--speaking { box-shadow: 0 0 0 2px #4ade80; }
.rk-tile video { width: 100%; height: 100%; object-fit: contain; }
.rk-label {
  position: absolute; bottom: 6px; left: 6px;
  background: rgba(0,0,0,0.6); color: #fafafa;
  padding: 2px 6px; font-size: 11px; border-radius: 4px;
}
</style>
