import { ref, watch, onBeforeUnmount, type Ref } from 'vue'

export function useAudioLevel(stream: Ref<MediaStream | null>, threshold = 0.015) {
  const isSpeaking = ref(false)
  let ctx: AudioContext | null = null
  let rafId: number | null = null

  function stop() {
    if (rafId !== null) { cancelAnimationFrame(rafId); rafId = null }
    if (ctx) { ctx.close(); ctx = null }
    isSpeaking.value = false
  }

  function start(s: MediaStream) {
    stop()
    if (!s.getAudioTracks().length) return
    try {
      ctx = new AudioContext()
      const analyser = ctx.createAnalyser()
      analyser.fftSize = 512
      ctx.createMediaStreamSource(s).connect(analyser)
      const data = new Uint8Array(analyser.frequencyBinCount)
      const tick = () => {
        analyser.getByteFrequencyData(data)
        const rms = Math.sqrt(data.reduce((acc, v) => acc + v * v, 0) / data.length) / 255
        isSpeaking.value = rms > threshold
        rafId = requestAnimationFrame(tick)
      }
      rafId = requestAnimationFrame(tick)
    } catch {
      // AudioContext unavailable (e.g. SSR)
    }
  }

  watch(stream, (s) => (s ? start(s) : stop()), { immediate: true })
  onBeforeUnmount(stop)

  return { isSpeaking }
}
