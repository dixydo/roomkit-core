package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/dixydo/roomkit/internal/auth"
	"github.com/dixydo/roomkit/internal/config"
	"github.com/dixydo/roomkit/internal/turn"
)

type iceServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

type iceConfig struct {
	ICEServers []iceServer `json:"iceServers"`
}

const credentialsTTL = 5 * time.Minute

// featuresHandler tells the frontend which optional features are enabled
// so the UI can hide buttons gracefully.
func featuresHandler(cfg config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		payload := map[string]any{
			"recording": cfg.Recording.Enabled(),
			"turn":      cfg.TURN.Enabled() || hasTURN(cfg.ICEServers),
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache, no-store")
		_ = json.NewEncoder(w).Encode(payload)
	}
}

func hasTURN(servers []config.ICEServerConfig) bool {
	for _, server := range servers {
		for _, url := range server.URLs {
			if strings.HasPrefix(strings.ToLower(url), "turn:") || strings.HasPrefix(strings.ToLower(url), "turns:") {
				return true
			}
		}
	}
	return false
}

// iceConfigHandler returns ICE servers config for the browser to use when
// constructing RTCPeerConnection. It uses ROOMKIT_ICE_SERVERS when present,
// otherwise includes public STUN and the embedded TURN when configured.
func iceConfigHandler(cfg config.Config, authCfg auth.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if authCfg.Enabled() {
			roomID := r.URL.Query().Get("room")
			if roomID == "" {
				http.Error(w, "missing room", http.StatusBadRequest)
				return
			}
			if _, err := auth.ValidateRoomToken(authCfg, auth.ExtractBearerToken(r), roomID, time.Now()); err != nil {
				http.Error(w, err.Error(), http.StatusUnauthorized)
				return
			}
		}

		if len(cfg.ICEServers) > 0 {
			servers := make([]iceServer, 0, len(cfg.ICEServers))
			for _, server := range cfg.ICEServers {
				servers = append(servers, iceServer{
					URLs:       server.URLs,
					Username:   server.Username,
					Credential: server.Credential,
				})
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-cache, no-store")
			_ = json.NewEncoder(w).Encode(iceConfig{ICEServers: servers})
			return
		}

		servers := []iceServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		}

		if cfg.TURN.Enabled() {
			user, pass := turn.Credentials(cfg.TURN.Secret, credentialsTTL)
			servers = append(servers, iceServer{
				URLs: []string{
					fmt.Sprintf("turn:%s:%d?transport=udp", cfg.TURN.PublicIP, cfg.TURN.Port),
				},
				Username:   user,
				Credential: pass,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache, no-store")
		_ = json.NewEncoder(w).Encode(iceConfig{ICEServers: servers})
	}
}
