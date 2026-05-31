import { useEffect, useRef, useState, useCallback } from 'react'
import {
  RoomkitRoom,
  type RoomkitOptions,
  type JoinOptions,
  type RemotePeer,
  type ChatMessage,
  type RecordingStatus,
} from '@dixydo/roomkit'

export interface UseRoomkitOptions extends RoomkitOptions {
  /** Pass to room.join(); default { audio: true, video: true } */
  joinOptions?: JoinOptions
  /** If false, you must call api.join() yourself. Default true. */
  autoJoin?: boolean
}

export interface UseRoomkitApi {
  room: RoomkitRoom | null
  status: 'idle' | 'joining' | 'joined' | 'error' | 'left'
  error: Error | null
  localPeerId: string
  localStream: MediaStream | null
  peers: Map<string, RemotePeer>
  chatMessages: ChatMessage[]
  recording: RecordingStatus
  micEnabled: boolean
  cameraEnabled: boolean
  screenSharing: boolean

  join: (opts?: JoinOptions) => Promise<void>
  leave: () => Promise<void>
  setMicEnabled: (b: boolean) => void
  setCamEnabled: (b: boolean) => void
  toggleScreenShare: () => Promise<void>
  setCameraDevice: (id: string) => Promise<void>
  setMicrophoneDevice: (id: string) => Promise<void>
  sendChat: (text: string) => void
  startRecording: () => void
  stopRecording: () => void
}

/**
 * React hook that mirrors the RoomkitRoom surface using React state.
 *
 * Construction inputs (serverUrl, roomId, token, peerId) are
 * captured on first mount and never re-read; pass them as stable values.
 * Hook-level state (peers, recording, etc.) is reactive.
 */
export function useRoomkit(options: UseRoomkitOptions): UseRoomkitApi {
  const roomRef = useRef<RoomkitRoom | null>(null)
  const optionsRef = useRef(options) // capture once

  const [status, setStatus] = useState<UseRoomkitApi['status']>('idle')
  const [error, setError] = useState<Error | null>(null)
  const [localPeerId, setLocalPeerId] = useState('')
  const [localStream, setLocalStream] = useState<MediaStream | null>(null)
  const [peers, setPeers] = useState<Map<string, RemotePeer>>(new Map())
  const [chatMessages, setChatMessages] = useState<ChatMessage[]>([])
  const [recording, setRecording] = useState<RecordingStatus>({
    state: 'idle', startedBy: '', startedAt: 0, url: '', error: '',
  })
  const [micEnabled, _setMicEnabled] = useState(true)
  const [cameraEnabled, _setCameraEnabled] = useState(true)
  const [screenSharing, setScreenSharing] = useState(false)

  useEffect(() => {
    const opts = optionsRef.current
    const room = new RoomkitRoom({
      serverUrl: opts.serverUrl,
      roomId: opts.roomId,
      token: opts.token,
      peerId: opts.peerId,
    })
    roomRef.current = room

    const offs: Array<() => void> = []
    offs.push(room.on('joined', ({ peerId }) => {
      setLocalPeerId(peerId)
      setLocalStream(room.localStream)
      setStatus('joined')
    }))
    offs.push(room.on('left', () => {
      setStatus('left')
      setLocalStream(null)
      setPeers(new Map())
    }))
    offs.push(room.on('peerJoined', () => setPeers(new Map(room.peers))))
    offs.push(room.on('peerLeft', () => setPeers(new Map(room.peers))))
    offs.push(room.on('trackPublished', () => setPeers(new Map(room.peers))))
    offs.push(room.on('trackUnpublished', () => setPeers(new Map(room.peers))))
    offs.push(room.on('chatMessage', () => setChatMessages([...room.chatMessages])))
    offs.push(room.on('recordingStateChange', (r) => setRecording(r)))
    offs.push(room.on('error', (e) => { setError(e); setStatus('error') }))

    if (opts.autoJoin !== false) {
      setStatus('joining')
      room.join(opts.joinOptions).catch((e) => { setError(e); setStatus('error') })
    }

    return () => {
      offs.forEach((off) => off())
      void room.leave()
      roomRef.current = null
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const join = useCallback(async (opts?: JoinOptions) => {
    if (!roomRef.current) return
    setStatus('joining')
    try {
      await roomRef.current.join(opts ?? optionsRef.current.joinOptions)
    } catch (e) {
      setError(e as Error); setStatus('error')
    }
  }, [])

  const leave = useCallback(async () => {
    await roomRef.current?.leave()
  }, [])

  const setMicEnabled = useCallback((b: boolean) => {
    roomRef.current?.setMicEnabled(b); _setMicEnabled(b)
  }, [])

  const setCamEnabled = useCallback((b: boolean) => {
    roomRef.current?.setCamEnabled(b); _setCameraEnabled(b)
  }, [])

  const toggleScreenShare = useCallback(async () => {
    if (!roomRef.current) return
    await roomRef.current.toggleScreenShare()
    setScreenSharing(roomRef.current.screenSharing)
  }, [])

  const setCameraDevice = useCallback(async (id: string) => {
    await roomRef.current?.setCameraDevice(id)
  }, [])

  const setMicrophoneDevice = useCallback(async (id: string) => {
    await roomRef.current?.setMicrophoneDevice(id)
  }, [])

  const sendChat = useCallback((text: string) => {
    roomRef.current?.sendChat(text)
  }, [])

  const startRecording = useCallback(() => roomRef.current?.startRecording(), [])
  const stopRecording = useCallback(() => roomRef.current?.stopRecording(), [])

  return {
    room: roomRef.current,
    status, error,
    localPeerId, localStream, peers, chatMessages, recording,
    micEnabled, cameraEnabled, screenSharing,
    join, leave,
    setMicEnabled, setCamEnabled, toggleScreenShare,
    setCameraDevice, setMicrophoneDevice,
    sendChat, startRecording, stopRecording,
  }
}
