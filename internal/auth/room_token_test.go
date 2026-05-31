package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

func TestValidateRoomToken(t *testing.T) {
	now := time.Unix(1000, 0)
	token := signToken(t, "secret", map[string]any{
		"room_id":     "support-123",
		"sub":         "user-456",
		"permissions": []string{"join", "record:start"},
		"exp":         now.Add(time.Minute).Unix(),
	})

	claims, err := ValidateRoomToken(Config{RoomTokenSecret: "secret"}, token, "support-123", now)
	if err != nil {
		t.Fatalf("expected token to validate: %v", err)
	}
	if claims.Subject != "user-456" {
		t.Fatalf("subject mismatch: %q", claims.Subject)
	}
	if !claims.HasPermission("record:start") {
		t.Fatal("expected record:start permission")
	}
}

func TestValidateRoomTokenRejectsWrongRoom(t *testing.T) {
	now := time.Unix(1000, 0)
	token := signToken(t, "secret", map[string]any{
		"room_id":     "support-123",
		"permissions": []string{"join"},
		"exp":         now.Add(time.Minute).Unix(),
	})

	if _, err := ValidateRoomToken(Config{RoomTokenSecret: "secret"}, token, "other-room", now); err == nil {
		t.Fatal("expected room mismatch to fail")
	}
}

func TestValidateRoomTokenRejectsMissingJoinPermission(t *testing.T) {
	now := time.Unix(1000, 0)
	token := signToken(t, "secret", map[string]any{
		"room_id":     "support-123",
		"permissions": []string{"record:start"},
		"exp":         now.Add(time.Minute).Unix(),
	})

	if _, err := ValidateRoomToken(Config{RoomTokenSecret: "secret"}, token, "support-123", now); err == nil {
		t.Fatal("expected missing join permission to fail")
	}
}

func TestValidateRoomTokenNoopsWhenDisabled(t *testing.T) {
	if _, err := ValidateRoomToken(Config{}, "", "support-123", time.Now()); err != nil {
		t.Fatalf("disabled auth should allow empty token: %v", err)
	}
}

func TestValidateRoomTokenRejectsExcessiveLifetime(t *testing.T) {
	now := time.Unix(1000, 0)
	token := signToken(t, "secret", map[string]any{
		"room_id":     "support-123",
		"permissions": []string{"join"},
		"exp":         now.Add(25 * time.Hour).Unix(),
	})

	cfg := Config{RoomTokenSecret: "secret", MaxTokenTTL: 24 * time.Hour}
	if _, err := ValidateRoomToken(cfg, token, "support-123", now); err == nil {
		t.Fatal("expected token with lifetime > MaxTokenTTL to fail")
	}
}

func TestValidateRoomTokenAcceptsWithinMaxTTL(t *testing.T) {
	now := time.Unix(1000, 0)
	token := signToken(t, "secret", map[string]any{
		"room_id":     "support-123",
		"permissions": []string{"join"},
		"exp":         now.Add(1 * time.Hour).Unix(),
	})

	cfg := Config{RoomTokenSecret: "secret", MaxTokenTTL: 24 * time.Hour}
	if _, err := ValidateRoomToken(cfg, token, "support-123", now); err != nil {
		t.Fatalf("expected token within MaxTokenTTL to validate: %v", err)
	}
}

func signToken(t *testing.T, secret string, claims map[string]any) string {
	t.Helper()
	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	headerBytes, err := json.Marshal(header)
	if err != nil {
		t.Fatal(err)
	}
	claimsBytes, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	enc := base64.RawURLEncoding
	signingInput := enc.EncodeToString(headerBytes) + "." + enc.EncodeToString(claimsBytes)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(signingInput))
	return signingInput + "." + enc.EncodeToString(mac.Sum(nil))
}
