package signaling

import (
	"errors"
	"log/slog"
	"sync"

	"github.com/dixydo/roomkit/internal/proto"
)

// Hub tracks WebSocket clients per room and broadcasts membership +
// chat messages. SFU negotiation (offer/answer/ice) is NOT handled here
// — those messages are dispatched to a Handler.
type Hub struct {
	log *slog.Logger

	MaxRooms        int // 0 = no limit
	MaxPeersPerRoom int // 0 = no limit

	mu    sync.RWMutex
	rooms map[string]map[string]*Client
}

func NewHub(log *slog.Logger) *Hub {
	return &Hub{
		log:   log,
		rooms: make(map[string]map[string]*Client),
	}
}

// Join registers a client in a room and returns the IDs of peers who were
// already present. Returns an error if server or room capacity is exceeded.
func (h *Hub) Join(roomID string, c *Client) ([]string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	room, ok := h.rooms[roomID]
	if !ok {
		if h.MaxRooms > 0 && len(h.rooms) >= h.MaxRooms {
			return nil, errors.New("server at capacity")
		}
		room = make(map[string]*Client)
		h.rooms[roomID] = room
	}
	if h.MaxPeersPerRoom > 0 && len(room) >= h.MaxPeersPerRoom {
		return nil, errors.New("room is full")
	}

	existing := make([]string, 0, len(room))
	for id := range room {
		existing = append(existing, id)
	}
	room[c.PeerID] = c

	h.log.Info("peer joined", "room", roomID, "peer", c.PeerID, "total", len(room))
	return existing, nil
}

func (h *Hub) Leave(roomID, peerID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	room, ok := h.rooms[roomID]
	if !ok {
		return
	}
	delete(room, peerID)
	remaining := len(room)
	if remaining == 0 {
		delete(h.rooms, roomID)
	}
	h.log.Info("peer left", "room", roomID, "peer", peerID, "remaining", remaining)
}

func (h *Hub) SendTo(roomID, toPeerID string, msg proto.Message) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	room, ok := h.rooms[roomID]
	if !ok {
		return
	}
	c, ok := room[toPeerID]
	if !ok {
		return
	}
	c.Send(msg)
}

func (h *Hub) Broadcast(roomID, exceptPeerID string, msg proto.Message) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	room, ok := h.rooms[roomID]
	if !ok {
		return
	}
	for id, c := range room {
		if id == exceptPeerID {
			continue
		}
		c.Send(msg)
	}
}

// BroadcastStatus sends msg to every peer in the room (including sender)
// — useful for state announcements that must reach everyone uniformly.
func (h *Hub) BroadcastStatus(roomID string, msg proto.Message) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	room, ok := h.rooms[roomID]
	if !ok {
		return
	}
	for _, c := range room {
		c.Send(msg)
	}
}
