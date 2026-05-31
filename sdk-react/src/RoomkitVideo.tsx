import { useEffect, useRef } from 'react'

interface Props {
  stream: MediaStream | null
  /** Mute the local audio playback (use for "self" tiles). */
  muted?: boolean
  className?: string
  style?: React.CSSProperties
}

/**
 * Small <video> wrapper that attaches a MediaStream via srcObject and
 * keeps it in sync if the prop changes.
 */
export function RoomkitVideo({ stream, muted, className, style }: Props) {
  const ref = useRef<HTMLVideoElement | null>(null)
  useEffect(() => {
    if (ref.current && stream && ref.current.srcObject !== stream) {
      ref.current.srcObject = stream
    }
  }, [stream])
  return (
    <video
      ref={ref}
      autoPlay
      playsInline
      muted={muted}
      className={className}
      style={style}
    />
  )
}
