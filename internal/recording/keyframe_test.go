package recording

import (
	"log/slog"
	"testing"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

func TestIsVP8Keyframe(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		want    bool
	}{
		{"keyframe, partition start", []byte{0x10, 0x00, 0x00, 0x00}, true},    // S=1, P=0
		{"interframe, partition start", []byte{0x10, 0x01, 0x00, 0x00}, false}, // S=1, P=1
		{"keyframe bits but not partition start", []byte{0x00, 0x00}, false},   // S=0
		{"empty payload", []byte{}, false},
	}
	for _, tt := range tests {
		got := isVP8Keyframe(&rtp.Packet{Payload: tt.payload})
		if got != tt.want {
			t.Errorf("%s: isVP8Keyframe = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// TestVideoWriterDeferredUntilKeyframe is the sync-critical guarantee: the video
// writer (and thus StartOffsetMs) must not be created on pre-keyframe packets,
// which ivfwriter discards anyway. Opening early would record the offset before
// the file's real content begins and desync audio.
func TestVideoWriterDeferredUntilKeyframe(t *testing.T) {
	dir := t.TempDir()
	sess, err := newSession("r", "p", dir, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	vp8 := webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8, ClockRate: 90000}
	inter := func(ts uint32) *rtp.Packet {
		return &rtp.Packet{Header: rtp.Header{Marker: true, Timestamp: ts}, Payload: []byte{0x10, 0x01, 0x00}}
	}
	key := func(ts uint32) *rtp.Packet {
		return &rtp.Packet{Header: rtp.Header{Marker: true, Timestamp: ts}, Payload: []byte{0x10, 0x00, 0x00}}
	}

	// Inter-frames before any keyframe must not open a writer.
	sess.WriteRTP("p", "v", webrtc.RTPCodecTypeVideo, vp8, inter(1000))
	sess.WriteRTP("p", "v", webrtc.RTPCodecTypeVideo, vp8, inter(2000))
	if n := len(sess.Tracks()); n != 0 {
		t.Fatalf("video writer opened before keyframe: %d tracks", n)
	}

	// First keyframe opens the writer.
	sess.WriteRTP("p", "v", webrtc.RTPCodecTypeVideo, vp8, key(3000))
	tr := sess.Tracks()
	if len(tr) != 1 || tr[0].Kind != webrtc.RTPCodecTypeVideo {
		t.Fatalf("expected 1 video track after keyframe, got %+v", tr)
	}
}

// TestAudioWriterOpensImmediately confirms audio is not subject to the keyframe
// gate — it has no keyframes and must start on the first packet.
func TestAudioWriterOpensImmediately(t *testing.T) {
	dir := t.TempDir()
	sess, err := newSession("r", "p", dir, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	opus := webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2}
	sess.WriteRTP("p", "a", webrtc.RTPCodecTypeAudio, opus,
		&rtp.Packet{Header: rtp.Header{Marker: true, Timestamp: 1000}, Payload: []byte{0xfc, 0x00, 0x00}})
	if n := len(sess.Tracks()); n != 1 {
		t.Fatalf("expected audio writer opened on first packet, got %d tracks", n)
	}
}
