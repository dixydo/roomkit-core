package turn

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	pionturn "github.com/pion/turn/v3"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestCredentialsFormat(t *testing.T) {
	const secret = "test-secret"
	before := time.Now().Add(time.Hour).Unix()
	username, password := Credentials(secret, time.Hour)
	after := time.Now().Add(time.Hour).Unix()

	parts := strings.SplitN(username, ":", 2)
	if len(parts) != 2 || parts[1] != "roomkit" {
		t.Fatalf("username = %q, want \"<expiry>:roomkit\"", username)
	}
	expiry, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		t.Fatalf("expiry not an int: %v", err)
	}
	if expiry < before || expiry > after {
		t.Errorf("expiry %d outside expected window [%d,%d]", expiry, before, after)
	}

	mac := hmac.New(sha1.New, []byte(secret))
	mac.Write([]byte(username))
	want := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	if password != want {
		t.Errorf("password = %q, want %q", password, want)
	}
}

func TestAuthHandlerAcceptsValidCredentials(t *testing.T) {
	const (
		secret = "shared-secret"
		realm  = "roomkit"
	)
	handler := authHandler(secret, discardLogger())
	username, password := Credentials(secret, time.Hour)

	key, ok := handler(username, realm, &net.UDPAddr{})
	if !ok {
		t.Fatal("handler rejected valid credentials")
	}
	want := pionturn.GenerateAuthKey(username, realm, password)
	if !hmac.Equal(key, want) {
		t.Error("handler returned unexpected auth key")
	}
}

func TestAuthHandlerRejectsBadInput(t *testing.T) {
	const secret = "shared-secret"
	handler := authHandler(secret, discardLogger())

	cases := map[string]string{
		"no colon":        "noexpiry",
		"non-numeric exp": "abc:roomkit",
		"expired":         strconv.FormatInt(time.Now().Add(-time.Minute).Unix(), 10) + ":roomkit",
		"empty":           "",
	}
	for name, username := range cases {
		t.Run(name, func(t *testing.T) {
			if _, ok := handler(username, "roomkit", &net.UDPAddr{}); ok {
				t.Errorf("handler accepted invalid username %q", username)
			}
		})
	}
}

func TestNewRejectsInvalidPublicIP(t *testing.T) {
	_, err := New(Config{PublicIP: "not-an-ip", Port: 3478, Secret: "s"}, discardLogger())
	if err == nil {
		t.Fatal("expected error for invalid public IP, got nil")
	}
}
