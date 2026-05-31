import {
  onMounted, onBeforeUnmount, ref, shallowRef, reactive, type Ref,
} from 'vue'
import {
  RoomkitRoom,
  type RoomkitOptions,
  type JoinOptions,
  type RemotePeer,
  type ChatMessage,
  type DeviceInfo,
  type RecordingStatus,
} from '@dixydo/roomkit'

export interface UseRoomkitOptions extends RoomkitOptions {
  joinOptions?: JoinOptions
  /** If false, you must call .join() yourself. Default true. */
  autoJoin?: boolean
}

export interface UseRoomkitApi {
  room: Ref<RoomkitRoom | null>
  status: Ref<'idle' | 'joining' | 'joined' | 'error' | 'left'>
  error: Ref<Error | null>
  localPeerId: Ref<string>
  localStream: Ref<MediaStream | null>
  /** Reactive Map. Use [...peers.entries()] in templates. */
  peers: Map<string, RemotePeer>
  chatMessages: Ref<ChatMessage[]>
  recording: Ref<RecordingStatus>
  micEnabled: Ref<boolean>
  cameraEnabled: Ref<boolean>
  screenSharing: Ref<boolean>
  micDeviceId: Ref<string>
  cameraDeviceId: Ref<string>

  join: (opts?: JoinOptions) => Promise<void>
  leave: () => Promise<void>
  setMicEnabled: (b: boolean) => void
  setCamEnabled: (b: boolean) => void
  toggleScreenShare: () => Promise<void>
  setCameraDevice: (id: string) => Promise<void>
  setMicrophoneDevice: (id: string) => Promise<void>
  listDevices: () => Promise<{ cameras: DeviceInfo[]; microphones: DeviceInfo[] }>
  sendChat: (text: string) => void
  startRecording: () => void
  stopRecording: () => void
}

/**
 * Vue 3 composable mirroring RoomkitRoom as reactive state.
 *
 * Construction inputs (serverUrl, roomId, token, peerId) are
 * captured on mount and not re-watched; pass stable values.
 */
export function useRoomkit(options: UseRoomkitOptions): UseRoomkitApi {
  const room = shallowRef<RoomkitRoom | null>(null)
  const status = ref<UseRoomkitApi['status']['value']>('idle')
  const error = ref<Error | null>(null)
  const localPeerId = ref('')
  const localStream = shallowRef<MediaStream | null>(null)
  const peers = reactive(new Map<string, RemotePeer>())
  const chatMessages = ref<ChatMessage[]>([])
  const recording = ref<RecordingStatus>({
    state: 'idle', startedBy: '', startedAt: 0, url: '', error: '',
  })
  const micEnabled = ref(true)
  const cameraEnabled = ref(true)
  const screenSharing = ref(false)
  const micDeviceId = ref('')
  const cameraDeviceId = ref('')

  const offs: Array<() => void> = []

  onMounted(() => {
    const r = new RoomkitRoom({
      serverUrl: options.serverUrl,
      roomId: options.roomId,
      token: options.token,
      peerId: options.peerId,
    })
    room.value = r

    offs.push(r.on('joined', ({ peerId }) => {
      localPeerId.value = peerId
      localStream.value = r.localStream
      status.value = 'joined'
      micDeviceId.value = r.localStream?.getAudioTracks()[0]?.getSettings().deviceId ?? ''
      cameraDeviceId.value = r.localStream?.getVideoTracks()[0]?.getSettings().deviceId ?? ''
    }))
    offs.push(r.on('left', () => {
      status.value = 'left'
      localStream.value = null
      peers.clear()
    }))
    const syncPeers = () => {
      peers.clear()
      r.peers.forEach((v, k) => peers.set(k, v))
    }
    offs.push(r.on('peerJoined', syncPeers))
    offs.push(r.on('peerLeft', syncPeers))
    offs.push(r.on('trackPublished', syncPeers))
    offs.push(r.on('trackUnpublished', syncPeers))
    offs.push(r.on('chatMessage', () => { chatMessages.value = [...r.chatMessages] }))
    offs.push(r.on('recordingStateChange', (v) => { recording.value = v }))
    offs.push(r.on('error', (e) => { error.value = e; status.value = 'error' }))

    if (options.autoJoin !== false) {
      status.value = 'joining'
      r.join(options.joinOptions).catch((e) => {
        error.value = e as Error
        status.value = 'error'
      })
    }
  })

  onBeforeUnmount(() => {
    offs.forEach((off) => off())
    void room.value?.leave()
    room.value = null
  })

  const join = async (opts?: JoinOptions) => {
    if (!room.value) return
    status.value = 'joining'
    try {
      await room.value.join(opts ?? options.joinOptions)
    } catch (e) {
      error.value = e as Error
      status.value = 'error'
    }
  }
  const leave = async () => { await room.value?.leave() }

  const setMicEnabled = (b: boolean) => {
    room.value?.setMicEnabled(b); micEnabled.value = b
  }
  const setCamEnabled = (b: boolean) => {
    room.value?.setCamEnabled(b); cameraEnabled.value = b
  }
  const toggleScreenShare = async () => {
    if (!room.value) return
    await room.value.toggleScreenShare()
    screenSharing.value = room.value.screenSharing
  }
  const setCameraDevice = async (id: string) => {
    await room.value?.setCameraDevice(id); cameraDeviceId.value = id
  }
  const setMicrophoneDevice = async (id: string) => {
    await room.value?.setMicrophoneDevice(id); micDeviceId.value = id
  }
  const listDevices = async () =>
    room.value?.listDevices() ?? { cameras: [], microphones: [] }
  const sendChat = (text: string) => room.value?.sendChat(text)
  const startRecording = () => room.value?.startRecording()
  const stopRecording = () => room.value?.stopRecording()

  return {
    room, status, error,
    localPeerId, localStream, peers, chatMessages, recording,
    micEnabled, cameraEnabled, screenSharing, micDeviceId, cameraDeviceId,
    join, leave,
    setMicEnabled, setCamEnabled, toggleScreenShare,
    setCameraDevice, setMicrophoneDevice, listDevices,
    sendChat, startRecording, stopRecording,
  }
}
