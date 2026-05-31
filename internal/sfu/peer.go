package sfu

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"

	"github.com/dixydo/roomkit/internal/proto"
	"github.com/dixydo/roomkit/internal/signaling"
)

// Peer represents one client in the SFU: it owns a publisher PC (created
// eagerly so we're ready for the client's pub-offer) and a subscriber PC
// (created lazily when there's at least one track to forward).
type Peer struct {
	ID         string
	sender     signaling.MessageSender
	room       *Room
	log        *slog.Logger
	api        *webrtc.API
	iceServers []webrtc.ICEServer

	pubPC *webrtc.PeerConnection

	subMu             sync.Mutex // protects subPC and subscribedSenders
	subPC             *webrtc.PeerConnection
	subscribedSenders map[string]*webrtc.RTPSender // key = "<publisherID>:<trackID>"
	subNeedsOffer     bool

	pubMu           sync.Mutex
	publishedTracks []*webrtc.TrackLocalStaticRTP
	videoRemotes    []*webrtc.TrackRemote // incoming video tracks, for on-demand PLI

	closeOnce sync.Once
	done      chan struct{}
	closed    atomic.Bool
}

func newPeer(id string, sender signaling.MessageSender, room *Room, api *webrtc.API, log *slog.Logger, iceServers []webrtc.ICEServer) *Peer {
	p := &Peer{
		ID:                id,
		sender:            sender,
		room:              room,
		log:               log.With("peer", id),
		api:               api,
		iceServers:        iceServers,
		subscribedSenders: make(map[string]*webrtc.RTPSender),
		done:              make(chan struct{}),
	}
	if err := p.setupPublisher(); err != nil {
		p.log.Error("setupPublisher", "err", err)
	}
	return p
}

func (p *Peer) PublishedTracks() []*webrtc.TrackLocalStaticRTP {
	p.pubMu.Lock()
	defer p.pubMu.Unlock()
	out := make([]*webrtc.TrackLocalStaticRTP, len(p.publishedTracks))
	copy(out, p.publishedTracks)
	return out
}

func (p *Peer) Close() {
	p.closeOnce.Do(func() {
		p.closed.Store(true)
		close(p.done)
		if p.pubPC != nil {
			_ = p.pubPC.Close()
		}
		p.subMu.Lock()
		if p.subPC != nil {
			_ = p.subPC.Close()
		}
		p.subMu.Unlock()
	})
}

// ---- Publisher --------------------------------------------------------------

func (p *Peer) setupPublisher() error {
	pc, err := p.api.NewPeerConnection(webrtc.Configuration{ICEServers: p.iceServers})
	if err != nil {
		return fmt.Errorf("new publisher pc: %w", err)
	}
	p.pubPC = pc

	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		p.sendICE(proto.RolePublisher, c)
	})

	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		p.log.Debug("publisher state", "state", s.String())
	})

	pc.OnTrack(func(remote *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		p.handleIncomingTrack(remote)
	})

	return nil
}

func (p *Peer) handleIncomingTrack(remote *webrtc.TrackRemote) {
	local, err := webrtc.NewTrackLocalStaticRTP(
		remote.Codec().RTPCodecCapability,
		remote.ID(),
		p.ID, // stream ID = publisher peer ID so subscribers can identify the owner
	)
	if err != nil {
		p.log.Warn("NewTrackLocalStaticRTP failed", "err", err)
		return
	}

	p.pubMu.Lock()
	p.publishedTracks = append(p.publishedTracks, local)
	p.pubMu.Unlock()

	p.log.Info("incoming track", "kind", remote.Kind().String(), "ssrc", remote.SSRC())

	// Fan out before starting the forwarder so subscribers get the offer with
	// this track even if the publisher is silent for the first few seconds.
	p.room.fanoutNewTrack(p.ID, local)

	// For video tracks, periodically request keyframes so late subscribers
	// can decode soon after their subscriber-PC connects, and remember the
	// remote track so a recording start can request a keyframe on demand.
	if remote.Kind() == webrtc.RTPCodecTypeVideo {
		p.pubMu.Lock()
		p.videoRemotes = append(p.videoRemotes, remote)
		p.pubMu.Unlock()
		go p.pliLoop(remote)
	}

	// RTP forwarding loop. Tees a parsed copy to the room's packet callback
	// (used by the recording subsystem); parsing is only paid for when the
	// callback is non-nil and the room actually has recording active.
	codec := remote.Codec().RTPCodecCapability
	trackID := remote.ID()
	kind := remote.Kind()
	cb := p.room.onPacket

	go func() {
		buf := make([]byte, 1500)
		for {
			n, _, err := remote.Read(buf)
			if err != nil {
				return
			}
			if _, err := local.Write(buf[:n]); err != nil {
				if !errors.Is(err, io.ErrClosedPipe) {
					p.log.Debug("local write", "err", err)
				}
				return
			}
			if cb != nil {
				pkt := &rtp.Packet{}
				if err := pkt.Unmarshal(buf[:n]); err == nil {
					cb(p.room.ID, p.ID, trackID, kind, codec, pkt)
				}
			}
		}
	}()
}

func (p *Peer) pliLoop(remote *webrtc.TrackRemote) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-p.done:
			return
		case <-ticker.C:
			if p.pubPC == nil {
				return
			}
			if state := p.pubPC.ConnectionState(); state == webrtc.PeerConnectionStateClosed ||
				state == webrtc.PeerConnectionStateFailed {
				return
			}
			if err := p.pubPC.WriteRTCP([]rtcp.Packet{
				&rtcp.PictureLossIndication{MediaSSRC: uint32(remote.SSRC())},
			}); err != nil {
				return
			}
		}
	}
}

// RequestKeyframe asks the publisher for an immediate key frame on every video
// track it publishes. Called when a recording starts so the IVF writer can
// begin at the next frame instead of waiting up to one pliLoop interval.
func (p *Peer) RequestKeyframe() {
	if p.pubPC == nil {
		return
	}
	p.pubMu.Lock()
	remotes := append([]*webrtc.TrackRemote(nil), p.videoRemotes...)
	p.pubMu.Unlock()
	for _, r := range remotes {
		if err := p.pubPC.WriteRTCP([]rtcp.Packet{
			&rtcp.PictureLossIndication{MediaSSRC: uint32(r.SSRC())},
		}); err != nil {
			p.log.Debug("request keyframe", "err", err)
		}
	}
}

func (p *Peer) handlePubOffer(sdp string) error {
	if err := p.pubPC.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer, SDP: sdp,
	}); err != nil {
		return fmt.Errorf("pub setRemote: %w", err)
	}
	answer, err := p.pubPC.CreateAnswer(nil)
	if err != nil {
		return fmt.Errorf("pub createAnswer: %w", err)
	}
	if err := p.pubPC.SetLocalDescription(answer); err != nil {
		return fmt.Errorf("pub setLocal: %w", err)
	}
	p.sender.Send(proto.Message{Type: proto.MsgPubAnswer, SDP: answer.SDP})
	return nil
}

// ---- Subscriber -------------------------------------------------------------

func (p *Peer) ensureSubscriber() error {
	if p.subPC != nil {
		return nil
	}
	pc, err := p.api.NewPeerConnection(webrtc.Configuration{ICEServers: p.iceServers})
	if err != nil {
		return fmt.Errorf("new subscriber pc: %w", err)
	}
	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		p.sendICE(proto.RoleSubscriber, c)
	})
	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		p.log.Debug("subscriber state", "state", s.String())
	})
	p.subPC = pc
	return nil
}

// AddSubscriberTrack adds `track` (owned by `publisherID`) to this peer's
// subscriber PC and renegotiates.
func (p *Peer) AddSubscriberTrack(publisherID string, track *webrtc.TrackLocalStaticRTP) error {
	p.subMu.Lock()
	defer p.subMu.Unlock()

	if p.closed.Load() {
		return nil
	}
	if err := p.ensureSubscriber(); err != nil {
		return err
	}

	sender, err := p.subPC.AddTrack(track)
	if err != nil {
		return fmt.Errorf("subscriber AddTrack: %w", err)
	}
	key := publisherID + ":" + track.ID()
	p.subscribedSenders[key] = sender

	return p.renegotiateSubscriberLocked()
}

// RemoveSubscriberTracksFrom removes every track owned by `publisherID`
// from this peer's subscriber PC and renegotiates if anything changed.
func (p *Peer) RemoveSubscriberTracksFrom(publisherID string) {
	p.subMu.Lock()
	defer p.subMu.Unlock()

	if p.subPC == nil {
		return
	}

	prefix := publisherID + ":"
	changed := false
	for key, sender := range p.subscribedSenders {
		if strings.HasPrefix(key, prefix) {
			_ = p.subPC.RemoveTrack(sender)
			delete(p.subscribedSenders, key)
			changed = true
		}
	}
	if changed {
		if err := p.renegotiateSubscriberLocked(); err != nil {
			p.log.Warn("renegotiate after RemoveSubscriberTracksFrom", "err", err)
		}
	}
}

// Caller must hold subMu.
func (p *Peer) renegotiateSubscriberLocked() error {
	if p.subPC.SignalingState() != webrtc.SignalingStateStable {
		p.subNeedsOffer = true
		return nil
	}

	p.subNeedsOffer = false
	offer, err := p.subPC.CreateOffer(nil)
	if err != nil {
		return fmt.Errorf("sub createOffer: %w", err)
	}
	if err := p.subPC.SetLocalDescription(offer); err != nil {
		return fmt.Errorf("sub setLocal: %w", err)
	}
	p.sender.Send(proto.Message{Type: proto.MsgSubOffer, SDP: offer.SDP})
	return nil
}

func (p *Peer) handleSubAnswer(sdp string) error {
	p.subMu.Lock()
	defer p.subMu.Unlock()
	if p.subPC == nil {
		return fmt.Errorf("sub-answer with no subscriber PC")
	}
	if err := p.subPC.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer, SDP: sdp,
	}); err != nil {
		return err
	}
	if p.subNeedsOffer {
		return p.renegotiateSubscriberLocked()
	}
	return nil
}

// ---- Common -----------------------------------------------------------------

func (p *Peer) handleICE(role proto.Role, candidateRaw json.RawMessage) error {
	if len(candidateRaw) == 0 {
		return nil
	}
	var init webrtc.ICECandidateInit
	if err := json.Unmarshal(candidateRaw, &init); err != nil {
		return fmt.Errorf("unmarshal ICE: %w", err)
	}
	var pc *webrtc.PeerConnection
	switch role {
	case proto.RolePublisher:
		pc = p.pubPC
	case proto.RoleSubscriber:
		p.subMu.Lock()
		pc = p.subPC
		p.subMu.Unlock()
	default:
		return fmt.Errorf("unknown ICE role %q", role)
	}
	if pc == nil {
		// candidate may arrive before the subscriber PC exists; drop silently
		return nil
	}
	return pc.AddICECandidate(init)
}

func (p *Peer) sendICE(role proto.Role, c *webrtc.ICECandidate) {
	b, err := json.Marshal(c.ToJSON())
	if err != nil {
		return
	}
	p.sender.Send(proto.Message{
		Type:      proto.MsgICE,
		Role:      role,
		Candidate: b,
	})
}

func (p *Peer) HandleMessage(msg proto.Message) {
	var err error
	switch msg.Type {
	case proto.MsgPubOffer:
		err = p.handlePubOffer(msg.SDP)
	case proto.MsgSubAnswer:
		err = p.handleSubAnswer(msg.SDP)
	case proto.MsgICE:
		err = p.handleICE(msg.Role, msg.Candidate)
	}
	if err != nil {
		p.log.Warn("handle message", "type", msg.Type, "err", err)
	}
}
