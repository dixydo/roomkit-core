package config

import (
	"encoding/json"
	"flag"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr           string
	LogLevel       string
	AllowedOrigins []string
	ICEServers     []ICEServerConfig

	Node      NodeConfig
	Auth      AuthConfig
	TURN      TURNConfig
	Recording RecordingConfig
	Limits    LimitsConfig
}

// NodeConfig identifies this SFU instance.
type NodeConfig struct {
	// NodeID is a stable human-readable name for this instance (defaults to hostname).
	NodeID string

	// PublicURL is the externally reachable base URL of this server, e.g.
	// "https://meet.example.com". Used to build download links for local recordings.
	// Defaults to http://localhost:<port> when not set.
	PublicURL string
}

type ICEServerConfig struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

// AuthConfig holds optional room token validation settings.
// When RoomTokenSecret is empty, roomkit operates in open/unauthenticated mode.
type AuthConfig struct {
	RoomTokenSecret string
	MaxTokenTTL     time.Duration // 0 = unlimited
}

func (a AuthConfig) Enabled() bool { return a.RoomTokenSecret != "" }

// TURNConfig holds embedded TURN server settings. If PublicIP and Secret are
// both empty the TURN server is not started and /api/ice-config returns STUN only.
type TURNConfig struct {
	PublicIP string
	Port     int
	Secret   string
	Realm    string
	MinPort  int
	MaxPort  int
}

// Enabled reports whether the TURN server should be started.
func (t TURNConfig) Enabled() bool {
	return t.PublicIP != "" && t.Secret != ""
}

// RecordingConfig holds server-side recording settings.
//
// Recording is enabled when LocalEnabled is true OR S3 is fully configured.
//   - LocalEnabled only: MP4s are saved to WorkDir and served at /recordings/*.
//   - S3 configured: MP4s are uploaded and served via presigned URL.
//   - Both set: S3 wins (local files are cleaned up after upload).
type RecordingConfig struct {
	WorkDir      string
	MaxDuration  time.Duration
	FFmpegPath   string
	LocalEnabled bool // enable recording without S3; files served from WorkDir

	S3Endpoint   string
	S3Region     string
	S3Bucket     string
	S3AccessKey  string
	S3SecretKey  string
	S3PublicBase string
	S3ObjectACL  string
	S3PresignTTL time.Duration
}

// S3Enabled reports whether S3 upload is fully configured.
func (r RecordingConfig) S3Enabled() bool {
	return r.S3Bucket != "" && r.S3AccessKey != "" && r.S3SecretKey != ""
}

// Enabled reports whether recording is active (local or S3 mode).
func (r RecordingConfig) Enabled() bool {
	return r.LocalEnabled || r.S3Enabled()
}

// LimitsConfig caps room and peer counts to prevent resource exhaustion.
// Zero values mean no limit (open/dev mode).
type LimitsConfig struct {
	MaxRooms        int // maximum concurrent rooms; 0 = unlimited
	MaxPeersPerRoom int // maximum peers per room; 0 = unlimited
}

func Load() Config {
	c := Config{
		Addr:           envOr("ROOMKIT_ADDR", ":8080"),
		LogLevel:       envOr("ROOMKIT_LOG_LEVEL", "info"),
		AllowedOrigins: splitCSV(envOr("ROOMKIT_ALLOWED_ORIGINS", "")),
		ICEServers:     loadICEServers(envOr("ROOMKIT_ICE_SERVERS", "")),
		Node: NodeConfig{
			NodeID:    resolveNodeID(),
			PublicURL: envOr("ROOMKIT_PUBLIC_URL", ""),
		},
		Auth: AuthConfig{
			RoomTokenSecret: envOr("ROOMKIT_ROOM_TOKEN_SECRET", ""),
			MaxTokenTTL:     time.Duration(envOrInt("ROOMKIT_ROOM_TOKEN_MAX_TTL_HOURS", 0)) * time.Hour,
		},
		TURN: TURNConfig{
			PublicIP: envOr("ROOMKIT_TURN_PUBLIC_IP", ""),
			Port:     envOrInt("ROOMKIT_TURN_PORT", 3478),
			Secret:   envOr("ROOMKIT_TURN_SECRET", ""),
			Realm:    envOr("ROOMKIT_TURN_REALM", "roomkit"),
			MinPort:  envOrInt("ROOMKIT_TURN_MIN_PORT", 49152),
			MaxPort:  envOrInt("ROOMKIT_TURN_MAX_PORT", 49200),
		},
		Recording: RecordingConfig{
			WorkDir:      envOr("ROOMKIT_REC_WORKDIR", "/tmp/roomkit-recordings"),
			MaxDuration:  time.Duration(envOrInt("ROOMKIT_REC_MAX_HOURS", 8)) * time.Hour,
			FFmpegPath:   envOr("ROOMKIT_FFMPEG_PATH", "ffmpeg"),
			LocalEnabled: envOr("ROOMKIT_REC_ENABLED", "") == "true",
			S3Endpoint:   envOr("ROOMKIT_S3_ENDPOINT", ""),
			S3Region:     envOr("ROOMKIT_S3_REGION", "us-east-1"),
			S3Bucket:     envOr("ROOMKIT_S3_BUCKET", ""),
			S3AccessKey:  envOr("ROOMKIT_S3_ACCESS_KEY", ""),
			S3SecretKey:  envOr("ROOMKIT_S3_SECRET_KEY", ""),
			S3PublicBase: envOr("ROOMKIT_S3_PUBLIC_BASE", ""),
			S3ObjectACL:  envOr("ROOMKIT_S3_OBJECT_ACL", ""),
			S3PresignTTL: time.Duration(envOrInt("ROOMKIT_REC_PRESIGN_TTL_HOURS", 24)) * time.Hour,
		},
		Limits: LimitsConfig{
			MaxRooms:        envOrInt("ROOMKIT_MAX_ROOMS", 0),
			MaxPeersPerRoom: envOrInt("ROOMKIT_MAX_PEERS_PER_ROOM", 0),
		},
	}

	originsCSV := strings.Join(c.AllowedOrigins, ",")

	flag.StringVar(&c.Addr, "addr", c.Addr, "HTTP listen address")
	flag.StringVar(&c.LogLevel, "log-level", c.LogLevel, "log level: debug, info, warn, error")
	flag.StringVar(&originsCSV, "allowed-origins", originsCSV, "comma-separated origins allowed to open WebSocket (empty = allow all, dev mode)")
	flag.StringVar(&c.TURN.PublicIP, "turn-public-ip", c.TURN.PublicIP, "TURN relay public IP (empty disables TURN)")
	flag.IntVar(&c.TURN.Port, "turn-port", c.TURN.Port, "TURN UDP listen port")
	flag.StringVar(&c.TURN.Secret, "turn-secret", c.TURN.Secret, "TURN HMAC shared secret (required when public-ip is set)")
	flag.StringVar(&c.TURN.Realm, "turn-realm", c.TURN.Realm, "TURN realm")
	flag.IntVar(&c.TURN.MinPort, "turn-min-port", c.TURN.MinPort, "TURN relay UDP port range start")
	flag.IntVar(&c.TURN.MaxPort, "turn-max-port", c.TURN.MaxPort, "TURN relay UDP port range end")
	flag.Parse()

	c.AllowedOrigins = splitCSV(originsCSV)
	return c
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func loadICEServers(raw string) []ICEServerConfig {
	if raw == "" {
		return nil
	}
	var servers []ICEServerConfig
	if err := json.Unmarshal([]byte(raw), &servers); err != nil {
		return nil
	}
	out := make([]ICEServerConfig, 0, len(servers))
	for _, server := range servers {
		if len(server.URLs) > 0 {
			out = append(out, server)
		}
	}
	return out
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envOrInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func resolveNodeID() string {
	if id := os.Getenv("ROOMKIT_NODE_ID"); id != "" {
		return id
	}
	if h, err := os.Hostname(); err == nil {
		return h
	}
	return "roomkit"
}
