package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dixydo/roomkit/internal/auth"
	"github.com/dixydo/roomkit/internal/config"
)

func TestResolvePublicURL(t *testing.T) {
	cases := []struct {
		name string
		cfg  config.Config
		want string
	}{
		{"explicit", config.Config{Node: config.NodeConfig{PublicURL: "https://meet.example.com"}}, "https://meet.example.com"},
		{"port only", config.Config{Addr: ":8080"}, "http://localhost:8080"},
		{"wildcard host", config.Config{Addr: "0.0.0.0:9000"}, "http://localhost:9000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolvePublicURL(tc.cfg); got != tc.want {
				t.Errorf("resolvePublicURL = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMP4Only(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := mp4Only(next)

	cases := map[string]int{
		"/room/sess/output.mp4": http.StatusOK,
		"/room/sess/track.ivf":  http.StatusNotFound,
		"/room/":                http.StatusNotFound,
		"/":                     http.StatusNotFound,
	}
	for path, want := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != want {
			t.Errorf("mp4Only(%q) = %d, want %d", path, rec.Code, want)
		}
	}
}

func TestWithCORS(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	t.Run("open mode echoes wildcard", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/x", nil)
		req.Header.Set("Origin", "https://anything.example")
		withCORS(nil, next).ServeHTTP(rec, req)
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
			t.Errorf("ACAO = %q, want *", got)
		}
	})

	t.Run("allowlisted origin echoed", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/x", nil)
		req.Header.Set("Origin", "https://app.example")
		withCORS([]string{"https://app.example"}, next).ServeHTTP(rec, req)
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example" {
			t.Errorf("ACAO = %q, want origin echo", got)
		}
		if rec.Header().Get("Vary") != "Origin" {
			t.Error("missing Vary: Origin on allowlisted response")
		}
	})

	t.Run("non-allowlisted origin not echoed", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/x", nil)
		req.Header.Set("Origin", "https://evil.example")
		withCORS([]string{"https://app.example"}, next).ServeHTTP(rec, req)
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("ACAO = %q, want empty for non-allowlisted origin", got)
		}
	})

	t.Run("preflight short-circuits", func(t *testing.T) {
		rec := httptest.NewRecorder()
		withCORS(nil, next).ServeHTTP(rec, httptest.NewRequest(http.MethodOptions, "/api/x", nil))
		if rec.Code != http.StatusNoContent {
			t.Errorf("OPTIONS = %d, want 204", rec.Code)
		}
	})
}

func TestHasTURN(t *testing.T) {
	cases := []struct {
		name    string
		servers []config.ICEServerConfig
		want    bool
	}{
		{"stun only", []config.ICEServerConfig{{URLs: []string{"stun:stun.l.google.com:19302"}}}, false},
		{"turn", []config.ICEServerConfig{{URLs: []string{"turn:turn.example.com:3478"}}}, true},
		{"turns uppercase", []config.ICEServerConfig{{URLs: []string{"TURNS:turn.example.com:5349"}}}, true},
		{"empty", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasTURN(tc.servers); got != tc.want {
				t.Errorf("hasTURN = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSFUICEServers(t *testing.T) {
	if got := sfuICEServers(config.Config{}); got != nil {
		t.Errorf("empty config should yield nil (default fallback applied in sfu.New), got %v", got)
	}
	cfg := config.Config{ICEServers: []config.ICEServerConfig{
		{URLs: []string{"turn:t.example:3478"}, Username: "u", Credential: "p"},
	}}
	got := sfuICEServers(cfg)
	if len(got) != 1 || got[0].Username != "u" || got[0].Credential != "p" {
		t.Errorf("sfuICEServers mapping wrong: %+v", got)
	}
}

func TestFeaturesHandler(t *testing.T) {
	cfg := config.Config{Recording: config.RecordingConfig{LocalEnabled: true}}
	rec := httptest.NewRecorder()
	featuresHandler(cfg)(rec, httptest.NewRequest(http.MethodGet, "/api/features", nil))

	var payload map[string]bool
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if !payload["recording"] {
		t.Error("recording should be true when LocalEnabled")
	}
	if payload["turn"] {
		t.Error("turn should be false when no TURN configured")
	}
}

func TestICEConfigHandlerOpenMode(t *testing.T) {
	cfg := config.Config{} // no ICEServers, no TURN
	rec := httptest.NewRecorder()
	iceConfigHandler(cfg, auth.Config{})(rec, httptest.NewRequest(http.MethodGet, "/api/ice-config", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var cfgResp iceConfig
	if err := json.Unmarshal(rec.Body.Bytes(), &cfgResp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(cfgResp.ICEServers) != 1 {
		t.Fatalf("want exactly the STUN fallback, got %d servers", len(cfgResp.ICEServers))
	}
}

func TestICEConfigHandlerAuthGate(t *testing.T) {
	authCfg := auth.Config{RoomTokenSecret: "secret"}

	t.Run("missing room", func(t *testing.T) {
		rec := httptest.NewRecorder()
		iceConfigHandler(config.Config{}, authCfg)(rec, httptest.NewRequest(http.MethodGet, "/api/ice-config", nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("missing token", func(t *testing.T) {
		rec := httptest.NewRecorder()
		iceConfigHandler(config.Config{}, authCfg)(rec, httptest.NewRequest(http.MethodGet, "/api/ice-config?room=r", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})
}
