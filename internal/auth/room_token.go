package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

var (
	ErrTokenRequired = errors.New("room token required")
	ErrInvalidToken  = errors.New("invalid room token")
)

type Config struct {
	RoomTokenSecret string
	MaxTokenTTL     time.Duration // maximum allowed token lifetime; 0 = unlimited
}

func (c Config) Enabled() bool { return c.RoomTokenSecret != "" }

type RoomClaims struct {
	RoomID      string   `json:"room_id,omitempty"`
	RoomIDCamel string   `json:"roomId,omitempty"`
	Subject     string   `json:"sub,omitempty"`
	Permissions []string `json:"permissions,omitempty"`
	ExpiresAt   int64    `json:"exp,omitempty"`
}

func (c RoomClaims) EffectiveRoomID() string {
	if c.RoomID != "" {
		return c.RoomID
	}
	return c.RoomIDCamel
}

func (c RoomClaims) HasPermission(permission string) bool {
	for _, p := range c.Permissions {
		if p == permission {
			return true
		}
	}
	return false
}

func ValidateRoomToken(cfg Config, token, roomID string, now time.Time) (RoomClaims, error) {
	if !cfg.Enabled() {
		return RoomClaims{}, nil
	}
	if strings.TrimSpace(token) == "" {
		return RoomClaims{}, ErrTokenRequired
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return RoomClaims{}, ErrInvalidToken
	}

	headerBytes, err := decode(parts[0])
	if err != nil {
		return RoomClaims{}, ErrInvalidToken
	}
	var header struct {
		Alg string `json:"alg"`
		Typ string `json:"typ"`
	}
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return RoomClaims{}, ErrInvalidToken
	}
	if header.Alg != "HS256" {
		return RoomClaims{}, fmt.Errorf("%w: unsupported alg", ErrInvalidToken)
	}

	signingInput := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, []byte(cfg.RoomTokenSecret))
	_, _ = mac.Write([]byte(signingInput))
	want := mac.Sum(nil)
	got, err := decode(parts[2])
	if err != nil {
		return RoomClaims{}, ErrInvalidToken
	}
	if !hmac.Equal(got, want) {
		return RoomClaims{}, ErrInvalidToken
	}

	payloadBytes, err := decode(parts[1])
	if err != nil {
		return RoomClaims{}, ErrInvalidToken
	}
	var claims RoomClaims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return RoomClaims{}, ErrInvalidToken
	}
	if claims.ExpiresAt <= 0 || now.Unix() >= claims.ExpiresAt {
		return RoomClaims{}, fmt.Errorf("%w: expired", ErrInvalidToken)
	}
	if cfg.MaxTokenTTL > 0 {
		remaining := time.Duration(claims.ExpiresAt-now.Unix()) * time.Second
		if remaining > cfg.MaxTokenTTL {
			return RoomClaims{}, fmt.Errorf("%w: token lifetime exceeds server maximum of %s", ErrInvalidToken, cfg.MaxTokenTTL)
		}
	}
	if claims.EffectiveRoomID() == "" {
		return RoomClaims{}, fmt.Errorf("%w: missing room_id", ErrInvalidToken)
	}
	if claims.EffectiveRoomID() != roomID {
		return RoomClaims{}, fmt.Errorf("%w: room mismatch", ErrInvalidToken)
	}
	if !claims.HasPermission("join") {
		return RoomClaims{}, fmt.Errorf("%w: missing join permission", ErrInvalidToken)
	}
	return claims, nil
}

// ExtractBearerToken extracts a JWT from a request.
// Prefers the Authorization: Bearer header; falls back to the ?token= query param.
// The query-param fallback exists because the browser WebSocket API does not support
// custom headers — tokens must be passed in the URL for WebSocket connections.
func ExtractBearerToken(r *http.Request) string {
	if token := r.URL.Query().Get("token"); token != "" {
		return token
	}
	authz := r.Header.Get("Authorization")
	if strings.HasPrefix(strings.ToLower(authz), "bearer ") {
		return strings.TrimSpace(authz[len("Bearer "):])
	}
	return ""
}

func decode(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}
