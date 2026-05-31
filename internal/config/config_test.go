package config

import (
	"testing"
)

func TestSplitCSV(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"a", []string{"a"}},
		{"a,b,c", []string{"a", "b", "c"}},
		{" a , b ", []string{"a", "b"}},
		{",,,", nil},
		{"a,,b", []string{"a", "b"}},
	}
	for _, tt := range tests {
		got := splitCSV(tt.in)
		if len(got) != len(tt.want) {
			t.Errorf("splitCSV(%q) = %v, want %v", tt.in, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("splitCSV(%q)[%d] = %q, want %q", tt.in, i, got[i], tt.want[i])
			}
		}
	}
}

func TestLoadICEServers(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		if got := loadICEServers(""); got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})
	t.Run("invalid json", func(t *testing.T) {
		if got := loadICEServers("not-json"); got != nil {
			t.Errorf("expected nil for invalid JSON, got %v", got)
		}
	})
	t.Run("valid two servers", func(t *testing.T) {
		raw := `[{"urls":["stun:stun.example.com:3478"]},{"urls":["turn:turn.example.com"],"username":"u","credential":"p"}]`
		got := loadICEServers(raw)
		if len(got) != 2 {
			t.Fatalf("expected 2 servers, got %d", len(got))
		}
		if got[0].URLs[0] != "stun:stun.example.com:3478" {
			t.Errorf("unexpected URL: %s", got[0].URLs[0])
		}
		if got[1].Username != "u" || got[1].Credential != "p" {
			t.Errorf("unexpected credential: %+v", got[1])
		}
	})
	t.Run("filters empty url lists", func(t *testing.T) {
		raw := `[{"urls":[]},{"urls":["stun:example.com"]}]`
		got := loadICEServers(raw)
		if len(got) != 1 {
			t.Errorf("expected 1 server after filtering, got %d", len(got))
		}
	})
}

func TestAuthConfigEnabled(t *testing.T) {
	if (AuthConfig{}).Enabled() {
		t.Error("empty AuthConfig should not be enabled")
	}
	if !(AuthConfig{RoomTokenSecret: "secret"}).Enabled() {
		t.Error("AuthConfig with secret should be enabled")
	}
}

func TestTURNConfigEnabled(t *testing.T) {
	if (TURNConfig{}).Enabled() {
		t.Error("empty TURNConfig should not be enabled")
	}
	if (TURNConfig{PublicIP: "1.2.3.4"}).Enabled() {
		t.Error("TURNConfig with only IP should not be enabled (missing secret)")
	}
	if (TURNConfig{Secret: "s"}).Enabled() {
		t.Error("TURNConfig with only secret should not be enabled (missing IP)")
	}
	if !(TURNConfig{PublicIP: "1.2.3.4", Secret: "s"}).Enabled() {
		t.Error("TURNConfig with IP+secret should be enabled")
	}
}

func TestRecordingConfigEnabled(t *testing.T) {
	if (RecordingConfig{}).Enabled() {
		t.Error("empty RecordingConfig should not be enabled")
	}
	if !(RecordingConfig{LocalEnabled: true}).Enabled() {
		t.Error("LocalEnabled should make Enabled() true")
	}

	full := RecordingConfig{S3Bucket: "b", S3AccessKey: "k", S3SecretKey: "s"}
	if !full.S3Enabled() {
		t.Error("fully configured S3 should be S3Enabled")
	}
	if !full.Enabled() {
		t.Error("S3 config should make Enabled() true")
	}

	partial := RecordingConfig{S3Bucket: "b", S3AccessKey: "k"} // missing secret
	if partial.S3Enabled() {
		t.Error("partial S3 config (missing secret) should not be S3Enabled")
	}
}
