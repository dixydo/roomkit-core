package recording

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"

	"github.com/dixydo/roomkit/internal/config"
	"github.com/dixydo/roomkit/internal/proto"
)

// StatusBroadcaster is implemented by the signaling layer so we can push
// state updates to every peer in a room.
type StatusBroadcaster interface {
	BroadcastStatus(roomID string, msg proto.Message)
}

type Manager struct {
	ctx       context.Context // server lifecycle context; used as parent for mux/upload operations
	cfg       config.RecordingConfig
	log       *slog.Logger
	s3        *S3 // nil when S3 is not configured
	publicURL string

	bcast       StatusBroadcaster
	keyframeReq func(roomID string) // asks the SFU for an immediate keyframe

	mu       sync.RWMutex
	sessions map[string]*Session // roomID -> active session (one at a time)
}

// New constructs a recording manager. ctx is the server's lifecycle context
// and is used as the parent for background mux/upload operations so they are
// cancelled on graceful shutdown. publicURL is the externally reachable base
// URL (e.g. "https://meet.example.com") — used to build download links when
// recordings are served locally.
func New(ctx context.Context, cfg config.RecordingConfig, publicURL string, log *slog.Logger) (*Manager, error) {
	m := &Manager{
		ctx:       ctx,
		cfg:       cfg,
		log:       log,
		publicURL: publicURL,
		sessions:  make(map[string]*Session),
	}
	if !cfg.Enabled() {
		log.Info("recording disabled (set ROOMKIT_REC_ENABLED=true or configure ROOMKIT_S3_BUCKET to enable)")
		return m, nil
	}
	if cfg.S3Enabled() {
		s3client, err := NewS3(ctx, cfg)
		if err != nil {
			return nil, fmt.Errorf("init s3: %w", err)
		}
		m.s3 = s3client
		log.Info("recording enabled (S3 upload)",
			"workdir", cfg.WorkDir, "bucket", cfg.S3Bucket, "endpoint", cfg.S3Endpoint,
			"max-duration", cfg.MaxDuration)
	} else {
		log.Info("recording enabled (local storage)",
			"workdir", cfg.WorkDir, "max-duration", cfg.MaxDuration,
			"serve-at", publicURL+"/recordings/")
	}
	return m, nil
}

func (m *Manager) Enabled() bool { return m.cfg.Enabled() }

func (m *Manager) SetBroadcaster(b StatusBroadcaster) { m.bcast = b }

// SetKeyframeRequester registers the hook used to ask the SFU for an immediate
// keyframe when a recording starts.
func (m *Manager) SetKeyframeRequester(fn func(roomID string)) { m.keyframeReq = fn }

// OnPacket is the callback the SFU invokes for every incoming RTP packet.
// If a session is active for that room, the packet is teed to its writer.
func (m *Manager) OnPacket(roomID, peerID, trackID string, kind webrtc.RTPCodecType, codec webrtc.RTPCodecCapability, pkt *rtp.Packet) {
	m.mu.RLock()
	sess := m.sessions[roomID]
	m.mu.RUnlock()
	if sess == nil {
		return
	}
	sess.WriteRTP(peerID, trackID, kind, codec, pkt)
}

// OnPeerLeave closes that peer's writers if a session is active.
func (m *Manager) OnPeerLeave(roomID, peerID string) {
	m.mu.RLock()
	sess := m.sessions[roomID]
	m.mu.RUnlock()
	if sess != nil {
		sess.PeerLeft(peerID)
	}
}

// Start begins recording for the given room. Errors when:
//   - recording is not configured
//   - a session is already active for this room
func (m *Manager) Start(roomID, startedBy string) error {
	if !m.Enabled() {
		return errors.New("recording is not configured on this server")
	}
	m.mu.Lock()
	if _, ok := m.sessions[roomID]; ok {
		m.mu.Unlock()
		return errors.New("recording already in progress for this room")
	}
	sess, err := newSession(roomID, startedBy, m.cfg.WorkDir, m.log)
	if err != nil {
		m.mu.Unlock()
		return err
	}
	m.sessions[roomID] = sess
	m.mu.Unlock()

	m.broadcastState(sess, proto.RecStateRecording, "", "")

	// Ask publishers for an immediate keyframe so video capture begins right
	// away. Without this, a recording started mid-call would discard every
	// video packet until the next periodic keyframe (up to a pliLoop interval),
	// leaving the start of the recording as audio-only.
	if m.keyframeReq != nil {
		m.keyframeReq(roomID)
	}

	// Max-duration safety net. Exits early when the session is stopped normally.
	go func() {
		timer := time.NewTimer(m.cfg.MaxDuration)
		defer timer.Stop()
		select {
		case <-timer.C:
			sess.log.Warn("recording hit max duration; stopping", "max", m.cfg.MaxDuration)
			_ = m.Stop(roomID, "")
		case <-sess.done:
		}
	}()

	return nil
}

// Stop closes writers, kicks off the async mux+upload pipeline, and
// returns immediately with state=processing. The terminal state-update
// (ready or failed) is broadcast later when processing completes.
func (m *Manager) Stop(roomID, requesterID string) error {
	m.mu.Lock()
	sess, ok := m.sessions[roomID]
	if !ok {
		m.mu.Unlock()
		return errors.New("no active recording for this room")
	}
	delete(m.sessions, roomID)
	m.mu.Unlock()

	sess.close()
	m.broadcastState(sess, proto.RecStateProcessing, "", "")

	go m.processAndFinalize(sess)
	return nil
}

// IsActive returns the active session for a room, or nil.
func (m *Manager) IsActive(roomID string) *Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[roomID]
}

// processAndFinalize runs FFmpeg mux then either uploads to S3 or keeps the
// file locally for HTTP serving. Broadcasts the final state when done.
// Uses the server's lifecycle context as parent so a graceful shutdown
// cancels in-progress mux/upload operations.
func (m *Manager) processAndFinalize(sess *Session) {
	ctx, cancel := context.WithTimeout(m.ctx, 2*time.Hour)
	defer cancel()

	tracks := sess.Tracks()
	if len(tracks) == 0 {
		sess.log.Warn("no tracks recorded; skipping mux")
		m.failSession(sess, "no media was published during recording")
		sess.cleanup()
		return
	}

	outPath := filepath.Join(sess.Dir, "output.mp4")
	if err := Mux(ctx, m.cfg.FFmpegPath, tracks, outPath, sess.log); err != nil {
		sess.log.Error("ffmpeg mux failed", "err", err)
		m.failSession(sess, "muxing failed: "+err.Error())
		sess.cleanup()
		return
	}

	var url string

	if m.s3 != nil {
		// Upload to S3 and get a presigned download URL.
		key := sanitize(sess.RoomID) + "/" + sess.ID + ".mp4"
		if _, err := m.s3.UploadFile(ctx, outPath, key, "video/mp4"); err != nil {
			sess.log.Error("s3 upload failed", "err", err)
			m.failSession(sess, "upload failed: "+err.Error())
			sess.cleanup()
			return
		}
		presignedURL, err := m.s3.PresignedURL(ctx, key, m.cfg.S3PresignTTL)
		if err != nil {
			sess.log.Error("s3 presign failed", "err", err)
			m.failSession(sess, "presign failed: "+err.Error())
			sess.cleanup()
			return
		}
		url = presignedURL
		sess.cleanup() // safe to delete local files after upload
	} else {
		// Local storage mode: derive the URL from the actual file path on disk
		// so it always matches what the file server will serve, regardless of
		// how roomID was sanitized.
		relPath, err := filepath.Rel(m.cfg.WorkDir, outPath)
		if err != nil {
			relPath = sanitize(sess.RoomID) + "/" + sess.ID + "/output.mp4"
		}
		base := strings.TrimRight(m.publicURL, "/")
		if base == "" {
			base = "http://localhost:8080"
		}
		url = base + "/recordings/" + filepath.ToSlash(relPath)

		var sizeBytes int64
		if fi, err := os.Stat(outPath); err == nil {
			sizeBytes = fi.Size()
		}
		sess.log.Info("recording saved locally", "path", outPath, "size_mb", sizeBytes/1024/1024)
	}

	sess.log.Info("recording ready", "url", url)
	sess.mu.Lock()
	sess.state = proto.RecStateReady
	sess.resultURL = url
	sess.mu.Unlock()
	m.broadcastState(sess, proto.RecStateReady, url, "")
}

func (m *Manager) failSession(sess *Session, msg string) {
	sess.mu.Lock()
	sess.state = proto.RecStateFailed
	sess.errorMsg = msg
	sess.mu.Unlock()
	m.broadcastState(sess, proto.RecStateFailed, "", msg)
}

func (m *Manager) broadcastState(sess *Session, st proto.RecordingState, url, errMsg string) {
	if m.bcast == nil {
		return
	}
	m.bcast.BroadcastStatus(sess.RoomID, proto.Message{
		Type:            proto.MsgRecordStatus,
		RecordState:     st,
		RecordStartedBy: sess.StartedBy,
		RecordStartedAt: sess.StartedAt.UnixMilli(),
		RecordURL:       url,
		RecordError:     errMsg,
	})
}
