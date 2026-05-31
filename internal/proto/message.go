// Package proto defines the wire format shared between the WebSocket
// signaling layer and the SFU. Putting Message in its own package breaks
// the import cycle between signaling (which dispatches) and sfu (which
// constructs responses).
package proto

import "encoding/json"

const (
	// Lifecycle
	MsgWelcome    = "welcome"
	MsgPeersList  = "peers-list"
	MsgPeerJoined = "peer-joined"
	MsgPeerLeft   = "peer-left"

	// SFU negotiation
	MsgPubOffer  = "pub-offer"  // client -> server: client publishes its tracks
	MsgPubAnswer = "pub-answer" // server -> client: answer to pub-offer
	MsgSubOffer  = "sub-offer"  // server -> client: server-initiated offer (initial or renegotiation)
	MsgSubAnswer = "sub-answer" // client -> server: answer to sub-offer
	MsgICE       = "ice"        // bidirectional, role field disambiguates which PC

	// Chat
	MsgChat = "chat"

	// Recording
	MsgRecordStart  = "record-start"  // client -> server: begin recording the room
	MsgRecordStop   = "record-stop"   // client -> server: stop & finalize recording
	MsgRecordStatus = "record-status" // server -> all in room: state changes
)

// RecordingState reflects the lifecycle of a single recording session.
type RecordingState string

const (
	RecStateIdle       RecordingState = "idle"
	RecStateRecording  RecordingState = "recording"
	RecStateProcessing RecordingState = "processing" // FFmpeg muxing / S3 upload
	RecStateReady      RecordingState = "ready"
	RecStateFailed     RecordingState = "failed"
)

// Role tells us which of a peer's two PeerConnections an ICE candidate
// applies to.
type Role string

const (
	RolePublisher  Role = "publisher"
	RoleSubscriber Role = "subscriber"
)

// Message is the union of all wire payloads. Optional fields are omitted
// when empty to match the TypeScript discriminator pattern on the frontend.
type Message struct {
	Type      string          `json:"type"`
	From      string          `json:"from,omitempty"`
	To        string          `json:"to,omitempty"`
	PeerID    string          `json:"peerId,omitempty"`
	Peers     []string        `json:"peers,omitempty"`
	SDP       string          `json:"sdp,omitempty"`
	Candidate json.RawMessage `json:"candidate,omitempty"`
	Role      Role            `json:"role,omitempty"`
	Text      string          `json:"text,omitempty"`
	Ts        int64           `json:"ts,omitempty"`

	// Recording
	RecordState     RecordingState `json:"recordState,omitempty"`
	RecordStartedBy string         `json:"recordStartedBy,omitempty"`
	RecordStartedAt int64          `json:"recordStartedAt,omitempty"`
	RecordURL       string         `json:"recordUrl,omitempty"`
	RecordError     string         `json:"recordError,omitempty"`
}
