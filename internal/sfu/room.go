package sfu

import (
	"log/slog"
	"sync"

	"github.com/pion/webrtc/v4"

	"github.com/dixydo/roomkit/internal/proto"
	"github.com/dixydo/roomkit/internal/signaling"
)

// Room holds every peer in a logical room and orchestrates cross-peer
// track forwarding.
type Room struct {
	ID  string
	log *slog.Logger
	api *webrtc.API

	onPacket   PacketCallback
	iceServers []webrtc.ICEServer

	mu    sync.RWMutex
	peers map[string]*Peer
}

func newRoom(id string, api *webrtc.API, log *slog.Logger, onPacket PacketCallback, iceServers []webrtc.ICEServer) *Room {
	return &Room{
		ID:         id,
		log:        log.With("room", id),
		api:        api,
		onPacket:   onPacket,
		iceServers: iceServers,
		peers:      make(map[string]*Peer),
	}
}

// AddPeer creates a Peer and subscribes it to every existing peer's
// published tracks. Returns the new Peer.
func (r *Room) AddPeer(peerID string, sender signaling.MessageSender) *Peer {
	p := newPeer(peerID, sender, r, r.api, r.log, r.iceServers)

	r.mu.Lock()
	existing := make([]*Peer, 0, len(r.peers))
	for _, other := range r.peers {
		existing = append(existing, other)
	}
	r.peers[peerID] = p
	r.mu.Unlock()

	// Subscribe new peer to every existing peer's already-published tracks.
	for _, other := range existing {
		for _, t := range other.PublishedTracks() {
			if err := p.AddSubscriberTrack(other.ID, t); err != nil {
				r.log.Warn("subscribe new peer to existing track failed",
					"new", peerID, "from", other.ID, "err", err)
			}
		}
	}

	return p
}

// RemovePeer tears down the peer's PCs and removes its tracks from
// every other peer's subscriber PC, renegotiating them.
// Returns true if the room is now empty.
func (r *Room) RemovePeer(peerID string) bool {
	r.mu.Lock()
	peer, ok := r.peers[peerID]
	if !ok {
		r.mu.Unlock()
		return len(r.peers) == 0
	}
	delete(r.peers, peerID)
	others := make([]*Peer, 0, len(r.peers))
	for _, p := range r.peers {
		others = append(others, p)
	}
	empty := len(r.peers) == 0
	r.mu.Unlock()

	peer.Close()
	for _, other := range others {
		other.RemoveSubscriberTracksFrom(peerID)
	}
	return empty
}

// fanoutNewTrack adds `track` (published by `publisherID`) to every other
// peer's subscriber PC, renegotiating them.
func (r *Room) fanoutNewTrack(publisherID string, track *webrtc.TrackLocalStaticRTP) {
	r.mu.RLock()
	others := make([]*Peer, 0, len(r.peers))
	for id, p := range r.peers {
		if id != publisherID {
			others = append(others, p)
		}
	}
	r.mu.RUnlock()

	for _, other := range others {
		if err := other.AddSubscriberTrack(publisherID, track); err != nil {
			r.log.Warn("fanout AddSubscriberTrack failed",
				"to", other.ID, "from", publisherID, "err", err)
		}
	}
}

// RequestKeyframe asks every peer in the room to emit an immediate key frame
// on its video tracks.
func (r *Room) RequestKeyframe() {
	r.mu.RLock()
	peers := make([]*Peer, 0, len(r.peers))
	for _, p := range r.peers {
		peers = append(peers, p)
	}
	r.mu.RUnlock()
	for _, p := range peers {
		p.RequestKeyframe()
	}
}

func (r *Room) IsEmpty() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.peers) == 0
}

func (r *Room) HandleMessage(peerID string, msg proto.Message) {
	r.mu.RLock()
	peer, ok := r.peers[peerID]
	r.mu.RUnlock()
	if !ok {
		return
	}
	peer.HandleMessage(msg)
}
