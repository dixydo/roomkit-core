export type RecordingState =
  | 'idle'
  | 'recording'
  | 'processing'
  | 'ready'
  | 'failed'

export interface RoomkitOptions {
  /** Base URL of the roomkit server, e.g. "https://meet.example.com". */
  serverUrl: string
  /** Room identifier. Anyone with the same id joins the same call. */
  roomId: string
  /**
   * Optional short-lived HS256 room token. Required when the server has
   * `ROOMKIT_ROOM_TOKEN_SECRET` configured. Generate with the server's secret:
   * payload must include `room_id`, `exp`, and `permissions: ["join"]`.
   * See the server's `.env.example` for the full token format.
   */
  token?: string
  /** Optional client-assigned peer id (otherwise the server assigns a UUID). */
  peerId?: string
}

export interface JoinOptions {
  /** Audio constraint or true for default mic. */
  audio?: boolean | MediaTrackConstraints
  /** Video constraint or true for default cam. */
  video?: boolean | MediaTrackConstraints
}

export interface RemotePeer {
  readonly id: string
  readonly stream: MediaStream
}

export interface ChatMessage {
  from: string
  text: string
  ts: number
  self: boolean
}

export interface RecordingStatus {
  state: RecordingState
  startedBy: string
  startedAt: number
  url: string
  error: string
}

export interface DeviceInfo {
  deviceId: string
  label: string
}

export type RoomkitEvents = {
  joined: { peerId: string }
  left: void
  peerJoined: { peerId: string }
  peerLeft: { peerId: string }
  trackPublished: { peerId: string; stream: MediaStream }
  trackUnpublished: { peerId: string }
  chatMessage: ChatMessage
  recordingStateChange: RecordingStatus
  error: Error
}
