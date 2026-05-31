package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/pion/webrtc/v4"

	"github.com/dixydo/roomkit/internal/auth"
	"github.com/dixydo/roomkit/internal/config"
	"github.com/dixydo/roomkit/internal/recording"
	"github.com/dixydo/roomkit/internal/sfu"
	"github.com/dixydo/roomkit/internal/signaling"
)

type Server struct {
	cfg  config.Config
	log  *slog.Logger
	http *http.Server
}

func New(ctx context.Context, cfg config.Config, log *slog.Logger) (*Server, error) {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	hub := signaling.NewHub(log)
	hub.MaxRooms = cfg.Limits.MaxRooms
	hub.MaxPeersPerRoom = cfg.Limits.MaxPeersPerRoom
	if cfg.Limits.MaxRooms > 0 || cfg.Limits.MaxPeersPerRoom > 0 {
		log.Info("connection limits active",
			"max_rooms", cfg.Limits.MaxRooms,
			"max_peers_per_room", cfg.Limits.MaxPeersPerRoom)
	}

	sfuMgr, err := sfu.New(log, sfuICEServers(cfg))
	if err != nil {
		return nil, err
	}

	recMgr, err := recording.New(ctx, cfg.Recording, resolvePublicURL(cfg), log)
	if err != nil {
		return nil, err
	}
	if recMgr.Enabled() {
		recMgr.SetBroadcaster(hub)
		sfuMgr.SetPacketCallback(recMgr.OnPacket)
		sfuMgr.SetPeerLeaveCallback(recMgr.OnPeerLeave)
		recMgr.SetKeyframeRequester(sfuMgr.RequestKeyframe)
	}

	// Recording controller is nil when not configured; signaling dispatch
	// returns a friendly "not configured" error to clients in that case.
	var recCtrl signaling.RecordingController
	if recMgr.Enabled() {
		recCtrl = recMgr
	}

	// Serve local recordings via /recordings/ when S3 is not configured.
	// Files are kept in WorkDir after muxing and served directly.
	if cfg.Recording.Enabled() && !cfg.Recording.S3Enabled() {
		mux.Handle("/recordings/", http.StripPrefix("/recordings/", mp4Only(http.FileServer(http.Dir(cfg.Recording.WorkDir)))))
		log.Info("local recording file server enabled", "path", cfg.Recording.WorkDir)
	}

	allowedOrigins := cfg.AllowedOrigins
	if len(allowedOrigins) == 0 {
		log.Warn("WebSocket origin check disabled (set ROOMKIT_ALLOWED_ORIGINS=https://your.domain in production)")
	} else {
		log.Info("WebSocket origin check enabled", "origins", allowedOrigins)
	}
	authCfg := auth.Config{
		RoomTokenSecret: cfg.Auth.RoomTokenSecret,
		MaxTokenTTL:     cfg.Auth.MaxTokenTTL,
	}
	if authCfg.Enabled() {
		log.Info("room token auth enabled", "max_ttl", cfg.Auth.MaxTokenTTL)
	}
	if recMgr.Enabled() && !authCfg.Enabled() {
		log.Warn("recording is enabled without room token auth; any participant can trigger recording")
	}
	mux.HandleFunc("GET /ws", func(w http.ResponseWriter, r *http.Request) {
		signaling.ServeWS(hub, sfuMgr, recCtrl, log, allowedOrigins, authCfg, w, r)
	})

	apiMux := http.NewServeMux()
	apiMux.HandleFunc("GET /api/ice-config", iceConfigHandler(cfg, authCfg))
	apiMux.HandleFunc("GET /api/features", featuresHandler(cfg))
	mux.Handle("/api/", withCORS(allowedOrigins, apiMux))

	srv := &Server{
		cfg: cfg,
		log: log,
		http: &http.Server{
			Addr:              cfg.Addr,
			Handler:           mux,
			ReadHeaderTimeout: 10 * time.Second,
		},
	}
	return srv, nil
}

func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		s.log.Info("listening", "addr", s.cfg.Addr)
		if err := s.http.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.http.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

// sfuICEServers converts the configured ICE servers to pion's type for use
// in server-side PeerConnections. Falls back to Google STUN when unconfigured.
func sfuICEServers(cfg config.Config) []webrtc.ICEServer {
	if len(cfg.ICEServers) == 0 {
		return nil // sfu.New applies the default Google STUN fallback
	}
	out := make([]webrtc.ICEServer, 0, len(cfg.ICEServers))
	for _, s := range cfg.ICEServers {
		out = append(out, webrtc.ICEServer{
			URLs:       s.URLs,
			Username:   s.Username,
			Credential: s.Credential,
		})
	}
	return out
}

// resolvePublicURL returns the externally reachable URL of this server.
// Uses ROOMKIT_PUBLIC_URL when set; otherwise derives a localhost URL from the
// listen address for use in local recording download links.
func resolvePublicURL(cfg config.Config) string {
	if cfg.Node.PublicURL != "" {
		return cfg.Node.PublicURL
	}
	addr := cfg.Addr
	switch {
	case strings.HasPrefix(addr, ":"):
		addr = "localhost" + addr
	case strings.HasPrefix(addr, "0.0.0.0:"):
		addr = "localhost:" + strings.TrimPrefix(addr, "0.0.0.0:")
	}
	return "http://" + addr
}

// mp4Only wraps a handler and returns 404 for anything that isn't a .mp4 file.
// Prevents directory listings and raw IVF/Ogg track files from being accessible.
func mp4Only(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, ".mp4") {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}
