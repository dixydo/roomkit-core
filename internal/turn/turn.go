// Package turn embeds a pion/turn server inside roomkit.
//
// Credentials follow the TURN REST API pattern (draft-uberti-behave-turn-rest):
// username = "<unix-expiry>:roomkit", password = base64(HMAC-SHA1(secret, username)).
// The HTTP /api/ice-config endpoint mints fresh short-lived credentials for
// clients; the TURN server validates them with the same shared secret.
package turn

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"

	pionturn "github.com/pion/turn/v3"
)

type Config struct {
	PublicIP string
	Port     int
	Secret   string
	Realm    string
	MinPort  int
	MaxPort  int
}

type Server struct {
	server      *pionturn.Server
	udpListener net.PacketConn
	log         *slog.Logger
}

// New starts a TURN server listening on UDP :Port and relaying allocations
// via PublicIP in port range [MinPort, MaxPort].
func New(cfg Config, log *slog.Logger) (*Server, error) {
	relayIP := net.ParseIP(cfg.PublicIP)
	if relayIP == nil {
		return nil, fmt.Errorf("invalid TURN public IP %q", cfg.PublicIP)
	}

	udpListener, err := net.ListenPacket("udp4", fmt.Sprintf("0.0.0.0:%d", cfg.Port))
	if err != nil {
		return nil, fmt.Errorf("listen udp %d: %w", cfg.Port, err)
	}

	s, err := pionturn.NewServer(pionturn.ServerConfig{
		Realm:       cfg.Realm,
		AuthHandler: authHandler(cfg.Secret, log),
		PacketConnConfigs: []pionturn.PacketConnConfig{{
			PacketConn: udpListener,
			RelayAddressGenerator: &pionturn.RelayAddressGeneratorPortRange{
				RelayAddress: relayIP,
				Address:      "0.0.0.0",
				MinPort:      uint16(cfg.MinPort),
				MaxPort:      uint16(cfg.MaxPort),
			},
		}},
	})
	if err != nil {
		_ = udpListener.Close()
		return nil, fmt.Errorf("turn.NewServer: %w", err)
	}

	log.Info("turn listening",
		"public-ip", cfg.PublicIP,
		"port", cfg.Port,
		"relay-range", fmt.Sprintf("%d-%d", cfg.MinPort, cfg.MaxPort),
		"realm", cfg.Realm,
	)

	return &Server{server: s, udpListener: udpListener, log: log}, nil
}

func (s *Server) Close() error {
	return s.server.Close()
}

// Credentials mints a TURN REST API username/password valid for `ttl`.
func Credentials(secret string, ttl time.Duration) (username, password string) {
	expiry := time.Now().Add(ttl).Unix()
	username = strconv.FormatInt(expiry, 10) + ":roomkit"
	mac := hmac.New(sha1.New, []byte(secret))
	mac.Write([]byte(username))
	password = base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return
}

func authHandler(secret string, log *slog.Logger) pionturn.AuthHandler {
	return func(username, realm string, srcAddr net.Addr) ([]byte, bool) {
		parts := strings.SplitN(username, ":", 2)
		if len(parts) != 2 {
			log.Debug("turn auth: malformed username", "username", username, "src", srcAddr)
			return nil, false
		}
		expiry, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			log.Debug("turn auth: bad expiry", "username", username, "src", srcAddr)
			return nil, false
		}
		if time.Now().Unix() > expiry {
			log.Debug("turn auth: expired", "username", username, "src", srcAddr)
			return nil, false
		}

		mac := hmac.New(sha1.New, []byte(secret))
		mac.Write([]byte(username))
		password := base64.StdEncoding.EncodeToString(mac.Sum(nil))

		return pionturn.GenerateAuthKey(username, realm, password), true
	}
}
