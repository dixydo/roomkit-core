export type SignalRole = 'publisher' | 'subscriber'

export interface SignalMessage {
  type: string
  from?: string
  to?: string
  peerId?: string
  peers?: string[]
  sdp?: string
  candidate?: RTCIceCandidateInit
  role?: SignalRole
  text?: string
  ts?: number
  recordState?: string
  recordStartedBy?: string
  recordStartedAt?: number
  recordUrl?: string
  recordError?: string
}

export type SignalListener = (msg: SignalMessage) => void

/**
 * Thin WebSocket wrapper that maps the roomkit signaling protocol onto
 * typed messages. Reconnect is intentionally left out — the SDK consumer
 * gets an `error` event and is responsible for calling join() again.
 */
export class SignalingClient {
  private ws: WebSocket | null = null
  private listeners: SignalListener[] = []
  peerId = ''

  constructor(
    private serverUrl: string,
    private roomId: string,
    private clientPeerId?: string,
    private token?: string,
  ) {}

  on(handler: SignalListener): () => void {
    this.listeners.push(handler)
    return () => {
      const i = this.listeners.indexOf(handler)
      if (i >= 0) this.listeners.splice(i, 1)
    }
  }

  send(msg: SignalMessage): void {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(msg))
    }
  }

  async connect(): Promise<void> {
    const base = this.serverUrl.replace(/\/$/, '')
    const wsBase = base.replace(/^http/, 'ws')
    const params = new URLSearchParams({ room: this.roomId })
    if (this.clientPeerId) params.set('peer', this.clientPeerId)
    if (this.token) params.set('token', this.token)
    const url = `${wsBase}/ws?${params.toString()}`

    return new Promise((resolve, reject) => {
      const ws = new WebSocket(url)
      this.ws = ws
      const onOpen = () => {
        ws.removeEventListener('open', onOpen)
        ws.removeEventListener('error', onError)
        resolve()
      }
      const onError = () => {
        ws.removeEventListener('open', onOpen)
        ws.removeEventListener('error', onError)
        reject(new Error('WebSocket connection failed'))
      }
      ws.addEventListener('open', onOpen)
      ws.addEventListener('error', onError)
      ws.addEventListener('message', (ev) => this.handleMessage(ev))
    })
  }

  private handleMessage(ev: MessageEvent): void {
    let msg: SignalMessage
    try {
      msg = JSON.parse(ev.data as string)
    } catch {
      return
    }
    if (msg.type === 'welcome' && msg.peerId) {
      this.peerId = msg.peerId
    }
    for (const h of this.listeners) h(msg)
  }

  close(): void {
    this.ws?.close(1000)
    this.ws = null
    this.listeners = []
  }
}
