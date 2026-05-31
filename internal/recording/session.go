// Package recording records SFU tracks to disk, muxes them into a single
// MP4 via FFmpeg, and uploads to an S3-compatible bucket (DigitalOcean
// Spaces by default).
//
// Lifecycle of a session:
//
//	Start    → state=recording, ticker for max-duration timeout
//	NewTrack → lazily creates per-(peerID, trackID) IVF/Ogg writer
//	PeerLeft → closes that peer's writers
//	Stop     → close writers; spawn goroutine: mux MP4 → upload S3 → state=ready
package recording

import (
	"crypto/rand"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media/ivfwriter"
	"github.com/pion/webrtc/v4/pkg/media/oggwriter"

	"github.com/dixydo/roomkit/internal/proto"
)

// TrackEntry describes one recorded track for FFmpeg muxing.
type TrackEntry struct {
	PeerID        string
	TrackID       string
	Kind          webrtc.RTPCodecType // audio or video
	FilePath      string
	StartOffsetMs int64 // when (relative to session start) this track first wrote a packet
}

// Session represents one in-progress (or finished) recording.
type Session struct {
	ID        string
	RoomID    string
	Dir       string
	StartedAt time.Time
	StartedBy string

	state     proto.RecordingState
	resultURL string
	errorMsg  string

	mu      sync.Mutex
	writers map[string]*trackWriter // key = peerID:trackID
	closed  bool
	done    chan struct{} // closed when session is stopped

	log *slog.Logger
}

type trackWriter struct {
	entry  TrackEntry
	writer rtpWriter
	once   sync.Once
}

// rtpWriter is what we wrap around IVFWriter / OggWriter.
type rtpWriter interface {
	WriteRTP(*rtp.Packet) error
	Close() error
}

func newSession(roomID, startedBy, baseDir string, log *slog.Logger) (*Session, error) {
	id := time.Now().UTC().Format("20060102T150405Z") + "_" + shortRand(6)
	// sanitize roomID so arbitrary room names can't traverse the filesystem.
	dir := filepath.Join(baseDir, sanitize(roomID), id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir session: %w", err)
	}
	return &Session{
		ID:        id,
		RoomID:    roomID,
		Dir:       dir,
		StartedAt: time.Now().UTC(),
		StartedBy: startedBy,
		state:     proto.RecStateRecording,
		writers:   make(map[string]*trackWriter),
		done:      make(chan struct{}),
		log:       log.With("session", id, "room", roomID),
	}, nil
}

func (s *Session) State() proto.RecordingState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

func (s *Session) setState(st proto.RecordingState) {
	s.mu.Lock()
	s.state = st
	s.mu.Unlock()
}

func (s *Session) Result() (string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.resultURL, s.errorMsg
}

func (s *Session) WriteRTP(peerID, trackID string, kind webrtc.RTPCodecType, codec webrtc.RTPCodecCapability, pkt *rtp.Packet) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	key := peerID + ":" + trackID
	tw, ok := s.writers[key]
	if !ok {
		// For video, defer opening the writer until the first key frame.
		// ivfwriter discards every packet before the first key frame anyway, so
		// opening on an earlier inter-frame packet would record StartOffsetMs too
		// early — shifting the video ahead of the audio by however long we waited
		// for the key frame (up to one pliLoop interval when recording is started
		// mid-call). Stamping the offset at the key frame keeps the tracks in sync.
		if kind == webrtc.RTPCodecTypeVideo && !isVP8Keyframe(pkt) {
			s.mu.Unlock()
			return
		}
		w, ext, err := newRTPWriter(s.Dir, peerID, trackID, kind, codec)
		if err != nil {
			s.log.Warn("create writer", "peer", peerID, "track", trackID, "err", err)
			s.mu.Unlock()
			return
		}
		tw = &trackWriter{
			entry: TrackEntry{
				PeerID:        peerID,
				TrackID:       trackID,
				Kind:          kind,
				FilePath:      filepath.Join(s.Dir, sanitize(peerID)+"_"+sanitize(trackID)+"."+ext),
				StartOffsetMs: time.Since(s.StartedAt).Milliseconds(),
			},
			writer: w,
		}
		s.writers[key] = tw
		s.log.Info("track writer opened", "peer", peerID, "kind", kind.String(), "file", tw.entry.FilePath)
	}
	s.mu.Unlock()

	if err := tw.writer.WriteRTP(pkt); err != nil {
		s.log.Debug("write rtp", "peer", peerID, "err", err)
	}
}

// PeerLeft closes writers for tracks owned by peerID. Allows late peers to
// re-publish without conflict (rare, but safer).
func (s *Session) PeerLeft(peerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	prefix := peerID + ":"
	for key, tw := range s.writers {
		if strings.HasPrefix(key, prefix) {
			tw.once.Do(func() { _ = tw.writer.Close() })
			// Don't delete from map — Tracks() needs the file path for FFmpeg.
		}
	}
}

// Tracks returns a snapshot of recorded tracks sorted for deterministic
// FFmpeg input order: audio tracks first (by peer ID), then video.
func (s *Session) Tracks() []TrackEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]TrackEntry, 0, len(s.writers))
	for _, tw := range s.writers {
		out = append(out, tw.entry)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind == webrtc.RTPCodecTypeAudio
		}
		return out[i].PeerID < out[j].PeerID
	})
	return out
}

// close flushes and closes every writer. Idempotent.
func (s *Session) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.done)
	for _, tw := range s.writers {
		tw.once.Do(func() { _ = tw.writer.Close() })
	}
}

// cleanup removes the session directory (call after successful upload).
func (s *Session) cleanup() {
	if err := os.RemoveAll(s.Dir); err != nil {
		s.log.Warn("cleanup dir", "err", err)
	}
}

// isVP8Keyframe reports whether pkt begins a VP8 key frame: the start of the
// first partition (S==1, PID==0) with the key-frame bit (P) cleared. This
// mirrors the gating ivfwriter applies internally, so we open the writer on the
// exact packet ivfwriter will treat as the first frame.
func isVP8Keyframe(pkt *rtp.Packet) bool {
	var vp8 codecs.VP8Packet
	if _, err := vp8.Unmarshal(pkt.Payload); err != nil {
		return false
	}
	return vp8.S == 1 && vp8.PID == 0 && len(vp8.Payload) > 0 && vp8.Payload[0]&0x01 == 0
}

// ---- writer factory ---------------------------------------------------------

func newRTPWriter(dir, peerID, trackID string, kind webrtc.RTPCodecType, codec webrtc.RTPCodecCapability) (rtpWriter, string, error) {
	switch kind {
	case webrtc.RTPCodecTypeVideo:
		if !strings.EqualFold(codec.MimeType, webrtc.MimeTypeVP8) {
			return nil, "", fmt.Errorf("unsupported video codec %q (only VP8)", codec.MimeType)
		}
		path := filepath.Join(dir, sanitize(peerID)+"_"+sanitize(trackID)+".ivf")
		// Write RTP timestamps straight through as PTS against the 90kHz video
		// clock. The default ivfwriter mode converts the timestamp to ms and
		// then re-scales by a 1/30 timebase, dividing by the clock twice
		// (effective ms/900 instead of ms/1000) — that stretches the video ~11%,
		// so it plays too slow and audio drifts ahead, with the picture freezing
		// once the (correctly-timed) audio ends. Direct PTS at 1/90000 fixes it.
		w, err := ivfwriter.New(path, ivfwriter.WithDirectPTS(), ivfwriter.WithFrameRate(1, 90000))
		return w, "ivf", err
	case webrtc.RTPCodecTypeAudio:
		if !strings.EqualFold(codec.MimeType, webrtc.MimeTypeOpus) {
			return nil, "", fmt.Errorf("unsupported audio codec %q (only Opus)", codec.MimeType)
		}
		path := filepath.Join(dir, sanitize(peerID)+"_"+sanitize(trackID)+".ogg")
		w, err := oggwriter.New(path, 48000, 2)
		return w, "ogg", err
	}
	return nil, "", fmt.Errorf("unknown track kind")
}

// sanitize keeps file-system-safe characters only.
func sanitize(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
			out = append(out, c)
		default:
			out = append(out, '-')
		}
	}
	return string(out)
}

func shortRand(n int) string {
	const alphabet = "abcdefghijkmnpqrstuvwxyz23456789"
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand: " + err.Error())
	}
	for i := range b {
		b[i] = alphabet[b[i]&31]
	}
	return string(b)
}
