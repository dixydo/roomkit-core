import { Emitter } from './emitter'
import { SignalingClient, type SignalMessage } from './signaling'
import type {
  ChatMessage,
  DeviceInfo,
  JoinOptions,
  RoomkitEvents,
  RoomkitOptions,
  RecordingState,
  RecordingStatus,
  RemotePeer,
} from './types'

const FALLBACK_ICE: RTCIceServer[] = [
  { urls: 'stun:stun.l.google.com:19302' },
]

const emptyRecording: RecordingStatus = {
  state: 'idle', startedBy: '', startedAt: 0, url: '', error: '',
}

/**
 * RoomkitRoom is the public client surface. Pattern mirrors 100ms / Agora:
 * construct → join() → wire up events → call media methods → leave().
 *
 * Subscribers see remote tracks via the `trackPublished` event, which
 * carries the publisher peerId and a MediaStream you can assign directly
 * to a <video>.srcObject (or pass into attachRemoteVideo helper).
 */
export class RoomkitRoom extends Emitter<RoomkitEvents> {
  readonly serverUrl: string
  readonly roomId: string
  readonly token?: string

  private signaling: SignalingClient
  private publisher: RTCPeerConnection | null = null
  private subscriber: RTCPeerConnection | null = null

  private _localStream: MediaStream | null = null
  private _peers = new Map<string, RemotePeer>()
  private _messages: ChatMessage[] = []
  private _recording: RecordingStatus = { ...emptyRecording }

  private cameraTrack: MediaStreamTrack | null = null
  private screenStream: MediaStream | null = null
  private _micOn = true
  private _camOn = true
  private _sharing = false

  private iceServers: RTCIceServer[] = FALLBACK_ICE

  constructor(options: RoomkitOptions) {
    super()
    this.serverUrl = options.serverUrl.replace(/\/$/, '')
    this.roomId = options.roomId
    this.token = options.token
    this.signaling = new SignalingClient(
      this.serverUrl,
      this.roomId,
      options.peerId,
      this.token,
    )
  }

  // ---- State accessors -----------------------------------------------------
  get localPeerId(): string { return this.signaling.peerId }
  get localStream(): MediaStream | null { return this._localStream }
  get peers(): ReadonlyMap<string, RemotePeer> { return this._peers }
  get chatMessages(): ReadonlyArray<ChatMessage> { return this._messages }
  get recording(): RecordingStatus { return { ...this._recording } }
  get micEnabled(): boolean { return this._micOn }
  get cameraEnabled(): boolean { return this._camOn }
  get screenSharing(): boolean { return this._sharing }

  // ---- Lifecycle -----------------------------------------------------------

  async join(opts: JoinOptions = { audio: true, video: true }): Promise<void> {
    // Fetch ICE config and request media in parallel.
    const [, stream] = await Promise.all([
      this.fetchIceServers().then((s) => { this.iceServers = s }),
      navigator.mediaDevices.getUserMedia({
        audio: opts.audio ?? true,
        video: opts.video ?? true,
      }),
    ])
    this._localStream = stream
    this.cameraTrack = stream.getVideoTracks()[0] ?? null

    this.signaling.on((m) => { this.handleSignal(m).catch((e) => this.emit('error', e)) })
    await this.signaling.connect()

    this.createPublisher()
    await this.publishLocal()

    this.emit('joined', { peerId: this.localPeerId })
  }

  async leave(): Promise<void> {
    this.screenStream?.getTracks().forEach((t) => t.stop())
    this.screenStream = null
    this.cameraTrack?.stop()
    this.cameraTrack = null
    this._localStream?.getTracks().forEach((t) => t.stop())
    this._localStream = null
    this.publisher?.close()
    this.publisher = null
    this.subscriber?.close()
    this.subscriber = null
    this.signaling.close()
    this._peers.clear()
    this.emit('left', undefined)
  }

  // ---- Media controls ------------------------------------------------------

  setMicEnabled(enabled: boolean): void {
    if (!this._localStream) return
    this._localStream.getAudioTracks().forEach((t) => (t.enabled = enabled))
    this._micOn = enabled
  }

  setCamEnabled(enabled: boolean): void {
    if (!this.cameraTrack) return
    this.cameraTrack.enabled = enabled
    this._camOn = enabled
  }

  async startScreenShare(): Promise<void> {
    if (this._sharing) return
    let stream: MediaStream
    try {
      stream = await navigator.mediaDevices.getDisplayMedia({ video: true, audio: false })
    } catch {
      return
    }
    const track = stream.getVideoTracks()[0]
    this.screenStream = stream
    track.onended = () => { this.stopScreenShare().catch(() => {}) }
    await this.replaceVideoTrack(track)
    this.setLocalVideoTrack(track)
    this._sharing = true
  }

  async stopScreenShare(): Promise<void> {
    if (!this._sharing) return
    this._sharing = false
    this.screenStream?.getTracks().forEach((t) => t.stop())
    this.screenStream = null
    await this.replaceVideoTrack(this.cameraTrack)
    this.setLocalVideoTrack(this.cameraTrack)
  }

  async toggleScreenShare(): Promise<void> {
    if (this._sharing) await this.stopScreenShare()
    else await this.startScreenShare()
  }

  async setCameraDevice(deviceId: string): Promise<void> {
    if (!this._localStream || this._sharing) return
    const stream = await navigator.mediaDevices.getUserMedia({
      video: { deviceId: { exact: deviceId } },
    })
    const track = stream.getVideoTracks()[0]
    this.cameraTrack?.stop()
    this.cameraTrack = track
    await this.replaceVideoTrack(track)
    this.setLocalVideoTrack(track)
  }

  async setMicrophoneDevice(deviceId: string): Promise<void> {
    if (!this._localStream) return
    const stream = await navigator.mediaDevices.getUserMedia({
      audio: { deviceId: { exact: deviceId } },
    })
    const track = stream.getAudioTracks()[0]
    track.enabled = this._micOn
    this._localStream.getAudioTracks().forEach((t) => {
      this._localStream!.removeTrack(t)
      t.stop()
    })
    this._localStream.addTrack(track)
    if (this.publisher) {
      const sender = this.publisher.getSenders().find((s) => s.track?.kind === 'audio')
      if (sender) await sender.replaceTrack(track)
    }
  }

  async listDevices(): Promise<{ cameras: DeviceInfo[]; microphones: DeviceInfo[] }> {
    const all = await navigator.mediaDevices.enumerateDevices()
    return {
      cameras: all
        .filter((d) => d.kind === 'videoinput')
        .map((d) => ({ deviceId: d.deviceId, label: d.label || 'Camera' })),
      microphones: all
        .filter((d) => d.kind === 'audioinput')
        .map((d) => ({ deviceId: d.deviceId, label: d.label || 'Microphone' })),
    }
  }

  // ---- Chat ----------------------------------------------------------------

  sendChat(text: string): void {
    const trimmed = text.trim()
    if (!trimmed) return
    const ts = Date.now()
    this.signaling.send({ type: 'chat', text: trimmed, ts })
    const msg: ChatMessage = { from: this.localPeerId, text: trimmed, ts, self: true }
    this._messages.push(msg)
    this.emit('chatMessage', msg)
  }

  // ---- Recording -----------------------------------------------------------

  startRecording(): void { this.signaling.send({ type: 'record-start' }) }
  stopRecording(): void { this.signaling.send({ type: 'record-stop' }) }

  // ---- Render helpers ------------------------------------------------------

  attachLocalVideo(el: HTMLVideoElement): void {
    if (!this._localStream) return
    el.srcObject = this._localStream
    el.autoplay = true
    el.playsInline = true
    el.muted = true
  }

  attachRemoteVideo(peerId: string, el: HTMLVideoElement): boolean {
    const p = this._peers.get(peerId)
    if (!p) return false
    el.srcObject = p.stream
    el.autoplay = true
    el.playsInline = true
    return true
  }

  // ---- Internal ------------------------------------------------------------

  private async fetchIceServers(): Promise<RTCIceServer[]> {
    try {
      const params = new URLSearchParams({ room: this.roomId })
      if (this.token) params.set('token', this.token)
      const resp = await fetch(`${this.serverUrl}/api/ice-config?${params.toString()}`)
      if (!resp.ok) throw new Error(`HTTP ${resp.status}`)
      const cfg = await resp.json()
      if (Array.isArray(cfg?.iceServers) && cfg.iceServers.length > 0) {
        return cfg.iceServers
      }
    } catch (e) {
      console.warn('[roomkit] ICE config fetch failed, using fallback STUN', e)
    }
    return FALLBACK_ICE
  }

  private createPublisher(): void {
    const pc = new RTCPeerConnection({ iceServers: this.iceServers })
    this.publisher = pc
    pc.onicecandidate = (ev) => {
      if (ev.candidate) {
        this.signaling.send({
          type: 'ice', role: 'publisher', candidate: ev.candidate.toJSON(),
        })
      }
    }
  }

  private async publishLocal(): Promise<void> {
    if (!this.publisher || !this._localStream) return
    for (const track of this._localStream.getTracks()) {
      this.publisher.addTrack(track, this._localStream)
    }
    const offer = await this.publisher.createOffer()
    await this.publisher.setLocalDescription(offer)
    this.signaling.send({ type: 'pub-offer', sdp: offer.sdp })
  }

  private ensureSubscriber(): void {
    if (this.subscriber) return
    const pc = new RTCPeerConnection({ iceServers: this.iceServers })
    this.subscriber = pc
    pc.onicecandidate = (ev) => {
      if (ev.candidate) {
        this.signaling.send({
          type: 'ice', role: 'subscriber', candidate: ev.candidate.toJSON(),
        })
      }
    }
    pc.ontrack = (ev) => {
      const stream = ev.streams[0]
      if (!stream) return
      const ownerId = stream.id
      const existing = this._peers.get(ownerId)
      if (existing && existing.stream.id === stream.id) return
      this._peers.set(ownerId, { id: ownerId, stream })
      this.emit('trackPublished', { peerId: ownerId, stream })
      ev.track.onended = () => {
        const cur = this._peers.get(ownerId)
        if (cur && cur.stream.getTracks().every((t) => t.readyState === 'ended')) {
          this._peers.delete(ownerId)
          this.emit('trackUnpublished', { peerId: ownerId })
        }
      }
    }
  }

  private async handleSignal(msg: SignalMessage): Promise<void> {
    switch (msg.type) {
      case 'welcome':
        // Captured in SignalingClient as peerId.
        break
      case 'peers-list':
        // Nothing — server will push sub-offers when needed.
        break
      case 'peer-joined':
        if (msg.peerId) this.emit('peerJoined', { peerId: msg.peerId })
        break
      case 'peer-left':
        if (msg.peerId) {
          this._peers.delete(msg.peerId)
          this.emit('peerLeft', { peerId: msg.peerId })
        }
        break
      case 'pub-answer':
        if (msg.sdp && this.publisher) {
          await this.publisher.setRemoteDescription({ type: 'answer', sdp: msg.sdp })
        }
        break
      case 'sub-offer':
        if (msg.sdp) {
          this.ensureSubscriber()
          await this.subscriber!.setRemoteDescription({ type: 'offer', sdp: msg.sdp })
          const answer = await this.subscriber!.createAnswer()
          await this.subscriber!.setLocalDescription(answer)
          this.signaling.send({ type: 'sub-answer', sdp: answer.sdp })
        }
        break
      case 'ice':
        if (msg.role && msg.candidate) {
          const pc = msg.role === 'publisher' ? this.publisher : this.subscriber
          if (pc) {
            try { await pc.addIceCandidate(msg.candidate) }
            catch (e) { console.warn('[roomkit] addIceCandidate failed', e) }
          }
        }
        break
      case 'chat':
        if (msg.from && msg.text) {
          const m: ChatMessage = {
            from: msg.from, text: msg.text, ts: msg.ts ?? Date.now(), self: false,
          }
          this._messages.push(m)
          this.emit('chatMessage', m)
        }
        break
      case 'record-status':
        this._recording = {
          state: (msg.recordState as RecordingState) ?? 'idle',
          startedBy: msg.recordStartedBy ?? '',
          startedAt: msg.recordStartedAt ?? 0,
          url: msg.recordUrl ?? '',
          error: msg.recordError ?? '',
        }
        this.emit('recordingStateChange', this.recording)
        break
    }
  }

  private async replaceVideoTrack(track: MediaStreamTrack | null): Promise<void> {
    if (!this.publisher) return
    const sender = this.publisher.getSenders().find((s) => s.track?.kind === 'video')
    if (sender) await sender.replaceTrack(track)
  }

  private setLocalVideoTrack(track: MediaStreamTrack | null): void {
    if (!this._localStream) return
    this._localStream.getVideoTracks().forEach((t) => this._localStream!.removeTrack(t))
    if (track) this._localStream.addTrack(track)
    this._localStream = new MediaStream(this._localStream.getTracks())
  }
}
