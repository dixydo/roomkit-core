package recording

import (
	"log/slog"
	"testing"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

func TestSanitize(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"hello", "hello"},
		{"hello-world_123", "hello-world_123"},
		{"ABC", "ABC"},
		{"room/name", "room-name"},
		{"a b!c", "a-b-c"},
		{"", ""},
		{"../../etc/passwd", "------etc-passwd"},
	}
	for _, tt := range tests {
		if got := sanitize(tt.in); got != tt.want {
			t.Errorf("sanitize(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestShortRand(t *testing.T) {
	const alphabet = "abcdefghijkmnpqrstuvwxyz23456789"
	for _, n := range []int{0, 1, 6, 16} {
		got := shortRand(n)
		if len(got) != n {
			t.Errorf("shortRand(%d): got len %d", n, len(got))
		}
		for _, c := range got {
			found := false
			for _, a := range alphabet {
				if c == a {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("shortRand(%d): char %q not in alphabet", n, c)
			}
		}
	}
}

func TestShortRandUniqueness(t *testing.T) {
	seen := make(map[string]bool, 100)
	for range 100 {
		id := shortRand(6)
		if seen[id] {
			t.Errorf("shortRand(6) collision: %q", id)
		}
		seen[id] = true
	}
}

func TestTracksSortedAudioFirst(t *testing.T) {
	sess := &Session{
		writers: map[string]*trackWriter{
			"p2:v": {entry: TrackEntry{PeerID: "p2", Kind: webrtc.RTPCodecTypeVideo}},
			"p2:a": {entry: TrackEntry{PeerID: "p2", Kind: webrtc.RTPCodecTypeAudio}},
			"p1:a": {entry: TrackEntry{PeerID: "p1", Kind: webrtc.RTPCodecTypeAudio}},
		},
	}
	tracks := sess.Tracks()
	if len(tracks) != 3 {
		t.Fatalf("expected 3 tracks, got %d", len(tracks))
	}
	if tracks[0].Kind != webrtc.RTPCodecTypeAudio {
		t.Errorf("tracks[0] should be audio, got %v", tracks[0].Kind)
	}
	if tracks[1].Kind != webrtc.RTPCodecTypeAudio {
		t.Errorf("tracks[1] should be audio, got %v", tracks[1].Kind)
	}
	if tracks[2].Kind != webrtc.RTPCodecTypeVideo {
		t.Errorf("tracks[2] should be video, got %v", tracks[2].Kind)
	}
	if tracks[0].PeerID > tracks[1].PeerID {
		t.Errorf("audio tracks not sorted by PeerID: %s > %s", tracks[0].PeerID, tracks[1].PeerID)
	}
}

func TestNewSession(t *testing.T) {
	dir := t.TempDir()
	sess, err := newSession("test-room", "user-1", dir, slog.Default())
	if err != nil {
		t.Fatalf("newSession: %v", err)
	}
	if sess.RoomID != "test-room" {
		t.Errorf("RoomID = %q, want test-room", sess.RoomID)
	}
	if sess.StartedBy != "user-1" {
		t.Errorf("StartedBy = %q, want user-1", sess.StartedBy)
	}
	if len(sess.ID) == 0 {
		t.Error("session ID should not be empty")
	}
	if sess.done == nil {
		t.Error("done channel should be initialised")
	}
	if sess.State() != "recording" {
		t.Errorf("initial state = %q, want recording", sess.State())
	}
}

func TestSessionPeerLeft(t *testing.T) {
	dir := t.TempDir()
	sess, _ := newSession("r", "p", dir, slog.Default())

	closed := false
	sess.mu.Lock()
	sess.writers["p1:track1"] = &trackWriter{
		entry:  TrackEntry{PeerID: "p1", TrackID: "track1"},
		writer: &fakeRTPWriter{closeFn: func() error { closed = true; return nil }},
	}
	sess.mu.Unlock()

	sess.PeerLeft("p1")
	if !closed {
		t.Error("PeerLeft should close the writer for that peer")
	}
}

type fakeRTPWriter struct {
	closeFn func() error
}

func (f *fakeRTPWriter) WriteRTP(_ *rtp.Packet) error { return nil }
func (f *fakeRTPWriter) Close() error                 { return f.closeFn() }
