package signaling

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/google/uuid"

	"github.com/dixydo/roomkit/internal/auth"
	"github.com/dixydo/roomkit/internal/proto"
)

const (
	sendBufferSize = 64
	writeTimeout   = 10 * time.Second
	readLimitBytes = 64 * 1024
)

// Handler processes signaling messages that the dumb hub doesn't itself
// understand (everything except chat + lifecycle). The SFU implements this.
type Handler interface {
	OnPeerJoin(roomID, peerID string, sender MessageSender)
	OnPeerLeave(roomID, peerID string)
	OnMessage(roomID, peerID string, msg proto.Message)
}

// RecordingController is invoked when clients ask to start/stop a recording.
type RecordingController interface {
	Start(roomID, startedBy string) error
	Stop(roomID, requesterID string) error
}

// MessageSender is what the SFU receives so it can push messages back to a
// specific client without depending on the Client struct directly.
type MessageSender interface {
	Send(msg proto.Message)
}

type Client struct {
	PeerID string
	RoomID string

	permissions []string // from JWT; nil means auth disabled (open mode)
	authEnabled bool

	conn *websocket.Conn
	send chan proto.Message
	log  *slog.Logger
}

// can reports whether this peer holds the given permission.
// When auth is disabled every peer can do everything.
func (c *Client) can(permission string) bool {
	if !c.authEnabled {
		return true
	}
	for _, p := range c.permissions {
		if p == permission {
			return true
		}
	}
	return false
}

func (c *Client) Send(msg proto.Message) {
	select {
	case c.send <- msg:
	default:
		c.log.Warn("send buffer full; closing slow consumer")
		_ = c.conn.Close(websocket.StatusPolicyViolation, "slow consumer")
	}
}

// ServeWS upgrades the HTTP request, drives the read/write pumps, and
// notifies hub + handler about lifecycle and messages. Recording controller
// is optional (nil disables record-start / record-stop dispatch).
func ServeWS(hub *Hub, handler Handler, rec RecordingController, log *slog.Logger, allowedOrigins []string, authCfg auth.Config, w http.ResponseWriter, r *http.Request) {
	roomID := r.URL.Query().Get("room")
	if roomID == "" {
		http.Error(w, "missing room", http.StatusBadRequest)
		return
	}
	claims, err := auth.ValidateRoomToken(authCfg, auth.ExtractBearerToken(r), roomID, time.Now())
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	var peerID string
	if !authCfg.Enabled() {
		// In open mode, ignore client-supplied peer ID to prevent spoofing.
		peerID = uuid.NewString()
	} else {
		supplied := r.URL.Query().Get("peer")
		switch {
		case supplied == "" && claims.Subject != "":
			peerID = claims.Subject
		case supplied == "":
			peerID = uuid.NewString()
		case claims.Subject != "" && supplied != claims.Subject && !claims.HasPermission("peer:custom"):
			http.Error(w, "peer does not match room token subject", http.StatusUnauthorized)
			return
		default:
			peerID = supplied
		}
	}

	opts := &websocket.AcceptOptions{}
	if len(allowedOrigins) == 0 {
		opts.InsecureSkipVerify = true
	} else {
		opts.OriginPatterns = allowedOrigins
	}

	conn, err := websocket.Accept(w, r, opts)
	if err != nil {
		log.Warn("ws accept failed", "err", err, "origin", r.Header.Get("Origin"))
		return
	}

	c := &Client{
		PeerID:      peerID,
		RoomID:      roomID,
		permissions: claims.Permissions,
		authEnabled: authCfg.Enabled(),
		conn:        conn,
		send:        make(chan proto.Message, sendBufferSize),
		log:         log.With("peer", peerID, "room", roomID),
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Welcome immediately so the client learns its assigned ID.
	wctx, wcancel := context.WithTimeout(ctx, writeTimeout)
	werr := wsjson.Write(wctx, conn, proto.Message{Type: proto.MsgWelcome, PeerID: peerID})
	wcancel()
	if werr != nil {
		c.log.Warn("welcome write failed", "err", werr)
		_ = conn.Close(websocket.StatusInternalError, "welcome failed")
		return
	}

	existing, joinErr := hub.Join(roomID, c)
	if joinErr != nil {
		c.log.Warn("join rejected", "err", joinErr)
		_ = conn.Close(websocket.StatusTryAgainLater, joinErr.Error())
		return
	}
	handler.OnPeerJoin(roomID, peerID, c)

	defer func() {
		handler.OnPeerLeave(roomID, peerID)
		hub.Leave(roomID, peerID)
		hub.Broadcast(roomID, peerID, proto.Message{Type: proto.MsgPeerLeft, PeerID: peerID})
		_ = conn.Close(websocket.StatusNormalClosure, "")
	}()

	c.Send(proto.Message{Type: proto.MsgPeersList, Peers: existing})
	hub.Broadcast(roomID, peerID, proto.Message{Type: proto.MsgPeerJoined, PeerID: peerID})

	go c.writePump(ctx)
	c.readPump(ctx, hub, handler, rec)
}

func (c *Client) writePump(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-c.send:
			if !ok {
				return
			}
			wctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := wsjson.Write(wctx, c.conn, msg)
			cancel()
			if err != nil {
				c.log.Debug("write failed", "err", err)
				return
			}
		}
	}
}

func (c *Client) readPump(ctx context.Context, hub *Hub, handler Handler, rec RecordingController) {
	c.conn.SetReadLimit(readLimitBytes)
	for {
		var msg proto.Message
		if err := wsjson.Read(ctx, c.conn, &msg); err != nil {
			var closeErr websocket.CloseError
			if !errors.As(err, &closeErr) && !errors.Is(err, context.Canceled) {
				c.log.Debug("read failed", "err", err)
			}
			return
		}
		msg.From = c.PeerID
		c.dispatch(hub, handler, rec, msg)
	}
}

func (c *Client) dispatch(hub *Hub, handler Handler, rec RecordingController, msg proto.Message) {
	switch msg.Type {
	case proto.MsgChat:
		if msg.Ts == 0 {
			msg.Ts = time.Now().UnixMilli()
		}
		hub.Broadcast(c.RoomID, c.PeerID, msg)
	case proto.MsgPubOffer, proto.MsgSubAnswer, proto.MsgICE:
		handler.OnMessage(c.RoomID, c.PeerID, msg)
	case proto.MsgRecordStart:
		if !c.can("record:start") {
			c.Send(proto.Message{
				Type:        proto.MsgRecordStatus,
				RecordState: proto.RecStateFailed,
				RecordError: "not authorized to start recording",
			})
			return
		}
		if rec == nil {
			c.Send(proto.Message{
				Type:        proto.MsgRecordStatus,
				RecordState: proto.RecStateFailed,
				RecordError: "recording is not configured on this server",
			})
			return
		}
		if err := rec.Start(c.RoomID, c.PeerID); err != nil {
			c.Send(proto.Message{
				Type:        proto.MsgRecordStatus,
				RecordState: proto.RecStateFailed,
				RecordError: err.Error(),
			})
		}
	case proto.MsgRecordStop:
		if !c.can("record:stop") {
			c.Send(proto.Message{
				Type:        proto.MsgRecordStatus,
				RecordState: proto.RecStateFailed,
				RecordError: "not authorized to stop recording",
			})
			return
		}
		if rec == nil {
			return
		}
		if err := rec.Stop(c.RoomID, c.PeerID); err != nil {
			c.Send(proto.Message{
				Type:        proto.MsgRecordStatus,
				RecordState: proto.RecStateFailed,
				RecordError: err.Error(),
			})
		}
	default:
		c.log.Warn("unknown message type", "type", msg.Type)
	}
}
