<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'

const props = withDefaults(
  defineProps<{ stream: MediaStream | null; muted?: boolean }>(),
  { muted: false }
)

const el = ref<HTMLVideoElement | null>(null)

function attach(s: MediaStream | null) {
  if (!el.value || !s) return
  const cur = el.value.srcObject as MediaStream | null
  // Skip if already attached to this exact stream — avoids decoder/jitter-buffer reset
  if (cur?.id === s.id) return
  el.value.srcObject = s
  // Don't call play() — autoplay attribute handles it, and an explicit play()
  // races with the browser's internal autoplay and throws AbortError, killing audio.
}

watch(() => props.stream, attach)
onMounted(() => attach(props.stream))
</script>

<template>
  <video ref="el" autoplay playsinline :muted="muted" />
</template>
