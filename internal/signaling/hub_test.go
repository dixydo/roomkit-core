package signaling

import (
	"log/slog"
	"testing"

	"github.com/dixydo/roomkit/internal/proto"
)

func newTestClient(id string) *Client {
	return &Client{
		PeerID: id,
		send:   make(chan proto.Message, 64),
		log:    slog.Default(),
	}
}

func TestHubJoinReturnsExistingPeers(t *testing.T) {
	h := NewHub(slog.Default())

	existing, err := h.Join("room1", newTestClient("p1"))
	if err != nil {
		t.Fatalf("first Join: %v", err)
	}
	if len(existing) != 0 {
		t.Errorf("expected 0 existing peers on first join, got %d", len(existing))
	}

	existing, err = h.Join("room1", newTestClient("p2"))
	if err != nil {
		t.Fatalf("second Join: %v", err)
	}
	if len(existing) != 1 || existing[0] != "p1" {
		t.Errorf("expected [p1] existing peers, got %v", existing)
	}
}

func TestHubMaxRoomsLimit(t *testing.T) {
	h := NewHub(slog.Default())
	h.MaxRooms = 2

	if _, err := h.Join("room1", newTestClient("p1")); err != nil {
		t.Fatalf("room1: %v", err)
	}
	if _, err := h.Join("room2", newTestClient("p2")); err != nil {
		t.Fatalf("room2: %v", err)
	}

	_, err := h.Join("room3", newTestClient("p3"))
	if err == nil {
		t.Error("expected error when creating room beyond MaxRooms")
	}

	// Joining an *existing* room at capacity is still allowed
	if _, err := h.Join("room1", newTestClient("p4")); err != nil {
		t.Errorf("joining existing room when at room-count limit should succeed: %v", err)
	}
}

func TestHubMaxPeersPerRoomLimit(t *testing.T) {
	h := NewHub(slog.Default())
	h.MaxPeersPerRoom = 2

	if _, err := h.Join("room1", newTestClient("p1")); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Join("room1", newTestClient("p2")); err != nil {
		t.Fatal(err)
	}

	_, err := h.Join("room1", newTestClient("p3"))
	if err == nil {
		t.Error("expected error when room is full")
	}
}

func TestHubLeaveRemovesPeer(t *testing.T) {
	h := NewHub(slog.Default())
	h.Join("room1", newTestClient("p1"))
	h.Join("room1", newTestClient("p2"))

	h.Leave("room1", "p1")

	// p3 joins; existing list should only contain p2
	existing, _ := h.Join("room1", newTestClient("p3"))
	for _, id := range existing {
		if id == "p1" {
			t.Error("p1 should have been removed by Leave")
		}
	}
}

func TestHubLeaveEmptyRoomCleansUp(t *testing.T) {
	h := NewHub(slog.Default())
	h.MaxRooms = 1

	h.Join("room1", newTestClient("p1"))
	h.Leave("room1", "p1") // room should be deleted

	// Now room2 can be created (the only slot is free)
	if _, err := h.Join("room2", newTestClient("p2")); err != nil {
		t.Errorf("room should have been freed after last peer left: %v", err)
	}
}

func TestHubBroadcastExcludesSender(t *testing.T) {
	h := NewHub(slog.Default())
	c1 := newTestClient("p1")
	c2 := newTestClient("p2")
	c3 := newTestClient("p3")
	h.Join("room1", c1)
	h.Join("room1", c2)
	h.Join("room1", c3)

	msg := proto.Message{Type: proto.MsgChat}
	h.Broadcast("room1", "p1", msg)

	if len(c1.send) != 0 {
		t.Error("sender (p1) should not receive its own broadcast")
	}
	if len(c2.send) != 1 {
		t.Errorf("p2 should receive 1 message, has %d", len(c2.send))
	}
	if len(c3.send) != 1 {
		t.Errorf("p3 should receive 1 message, has %d", len(c3.send))
	}
}

func TestHubBroadcastStatusReachesAll(t *testing.T) {
	h := NewHub(slog.Default())
	c1 := newTestClient("p1")
	c2 := newTestClient("p2")
	h.Join("room1", c1)
	h.Join("room1", c2)

	msg := proto.Message{Type: proto.MsgRecordStatus}
	h.BroadcastStatus("room1", msg)

	if len(c1.send) != 1 {
		t.Errorf("c1 should receive BroadcastStatus, has %d messages", len(c1.send))
	}
	if len(c2.send) != 1 {
		t.Errorf("c2 should receive BroadcastStatus, has %d messages", len(c2.send))
	}
}

func TestHubBroadcastUnknownRoomIsNoOp(t *testing.T) {
	h := NewHub(slog.Default())
	// Should not panic
	h.Broadcast("nonexistent", "p1", proto.Message{Type: proto.MsgChat})
	h.BroadcastStatus("nonexistent", proto.Message{Type: proto.MsgRecordStatus})
}
