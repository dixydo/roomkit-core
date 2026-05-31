// Package sfu implements a Selective Forwarding Unit on top of pion/webrtc.
//
// Each client holds two PeerConnections to the server:
//
//	publisher PC  — sends the client's own tracks (audio + video).
//	subscriber PC — receives every other peer's tracks, server-initiated.
//
// The server reads RTP packets off each publisher's incoming tracks and
// rewrites them onto local TrackLocalStaticRTP instances that are added to
// every other peer's subscriber PC. When a peer joins or leaves, the server
// renegotiates affected subscribers with fresh sub-offer messages.
//
// Codec policy: VP8 + Opus only. This keeps recording (IVF + Ogg writers)
// straightforward and avoids browser-specific H.264/VP9 quirks.
package sfu

import (
	"log/slog"
	"sync"

	"github.com/pion/interceptor"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"

	"github.com/dixydo/roomkit/internal/proto"
	"github.com/dixydo/roomkit/internal/signaling"
)

// PacketCallback is invoked for every incoming RTP packet from a publisher.
// Used to tee tracks into the recording subsystem.
type PacketCallback func(
	roomID, peerID, trackID string,
	kind webrtc.RTPCodecType,
	codec webrtc.RTPCodecCapability,
	pkt *rtp.Packet,
)

// PeerLeaveCallback is invoked when a peer disconnects from a room.
type PeerLeaveCallback func(roomID, peerID string)

// Manager owns one MediaEngine + API and a set of rooms.
type Manager struct {
	log        *slog.Logger
	api        *webrtc.API
	iceServers []webrtc.ICEServer

	onPacket    PacketCallback
	onPeerLeave PeerLeaveCallback

	mu    sync.RWMutex
	rooms map[string]*Room
}

// New creates an SFU manager. iceServers are used for all server-side
// PeerConnections (publisher and subscriber). Pass nil to use a default
// Google STUN fallback.
func New(log *slog.Logger, iceServers []webrtc.ICEServer) (*Manager, error) {
	if len(iceServers) == 0 {
		iceServers = []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		}
	}

	m := &webrtc.MediaEngine{}

	// Opus audio (PT 111 matches Chrome's offer).
	if err := m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeOpus,
			ClockRate:   48000,
			Channels:    2,
			SDPFmtpLine: "minptime=10;useinbandfec=1",
		},
		PayloadType: 111,
	}, webrtc.RTPCodecTypeAudio); err != nil {
		return nil, err
	}

	videoRTCPFeedback := []webrtc.RTCPFeedback{
		{Type: "goog-remb"},
		{Type: "ccm", Parameter: "fir"},
		{Type: "nack"},
		{Type: "nack", Parameter: "pli"},
	}
	if err := m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:     webrtc.MimeTypeVP8,
			ClockRate:    90000,
			RTCPFeedback: videoRTCPFeedback,
		},
		PayloadType: 96,
	}, webrtc.RTPCodecTypeVideo); err != nil {
		return nil, err
	}

	// A custom MediaEngine disables Pion's automatic default interceptors, so
	// register them explicitly against the same engine. The Sender Report
	// interceptor is what makes audio/video lip-sync work: it emits RTCP SRs
	// carrying the NTP↔RTP timestamp mapping the receiving browser needs to
	// align the separately-clocked Opus (48kHz) and VP8 (90kHz) streams.
	// Without it the browser cannot sync the two tracks and audio drifts
	// behind video. This set also restores NACK and TWCC.
	i := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(m, i); err != nil {
		return nil, err
	}

	api := webrtc.NewAPI(webrtc.WithMediaEngine(m), webrtc.WithInterceptorRegistry(i))

	return &Manager{
		log:        log,
		api:        api,
		iceServers: iceServers,
		rooms:      make(map[string]*Room),
	}, nil
}

// SetPacketCallback registers a hook called for every RTP packet flowing
// through any publisher. Used by the recording manager.
func (m *Manager) SetPacketCallback(cb PacketCallback) { m.onPacket = cb }

// SetPeerLeaveCallback registers a hook called when a peer leaves a room.
func (m *Manager) SetPeerLeaveCallback(cb PeerLeaveCallback) { m.onPeerLeave = cb }

// RequestKeyframe asks every publisher in the given room for an immediate key
// frame. No-op if the room does not exist. Used by the recording subsystem so a
// recording started mid-call captures video without waiting for the next
// periodic keyframe.
func (m *Manager) RequestKeyframe(roomID string) {
	m.mu.RLock()
	room, ok := m.rooms[roomID]
	m.mu.RUnlock()
	if ok {
		room.RequestKeyframe()
	}
}

// OnPeerJoin implements signaling.Handler.
func (m *Manager) OnPeerJoin(roomID, peerID string, sender signaling.MessageSender) {
	m.mu.Lock()
	room, ok := m.rooms[roomID]
	if !ok {
		room = newRoom(roomID, m.api, m.log, m.onPacket, m.iceServers)
		m.rooms[roomID] = room
	}
	m.mu.Unlock()

	room.AddPeer(peerID, sender)
}

// OnPeerLeave implements signaling.Handler.
func (m *Manager) OnPeerLeave(roomID, peerID string) {
	m.mu.RLock()
	room, ok := m.rooms[roomID]
	m.mu.RUnlock()
	if !ok {
		return
	}

	empty := room.RemovePeer(peerID)

	if cb := m.onPeerLeave; cb != nil {
		cb(roomID, peerID)
	}

	if empty {
		m.mu.Lock()
		// Re-check under write-lock: a peer may have joined between
		// RemovePeer returning empty and us acquiring this lock.
		if r2, ok := m.rooms[roomID]; ok && r2 == room && room.IsEmpty() {
			delete(m.rooms, roomID)
		}
		m.mu.Unlock()
	}
}

// OnMessage implements signaling.Handler. Dispatches to the peer.
func (m *Manager) OnMessage(roomID, peerID string, msg proto.Message) {
	m.mu.RLock()
	room, ok := m.rooms[roomID]
	m.mu.RUnlock()
	if !ok {
		return
	}
	room.HandleMessage(peerID, msg)
}
