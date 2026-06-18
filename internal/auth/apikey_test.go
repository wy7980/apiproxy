package auth

import (
	"net/http"
	"testing"
)

func TestAuthenticate(t *testing.T) {
	store := NewKeyStore([][2]string{{"key-a", "agent-a"}, {"key-b", "agent-b"}})

	r1, _ := http.NewRequest(http.MethodGet, "/", nil)
	r1.Header.Set("Authorization", "Bearer key-a")
	if id, ok := store.Authenticate(r1); !ok || id != "agent-a" {
		t.Fatalf("Authenticate() = %q,%v, want agent-a,true", id, ok)
	}

	r2, _ := http.NewRequest(http.MethodGet, "/", nil)
	if _, ok := store.Authenticate(r2); ok {
		t.Fatal("Authenticate() should fail without header")
	}

	r3, _ := http.NewRequest(http.MethodGet, "/", nil)
	r3.Header.Set("Authorization", "Bearer invalid")
	if _, ok := store.Authenticate(r3); ok {
		t.Fatal("Authenticate() should fail for unknown key")
	}
}

func TestAuthenticateAnthropic(t *testing.T) {
	store := NewKeyStore([][2]string{{"key-a", "agent-a"}, {"key-b", "agent-b"}})

	r1, _ := http.NewRequest(http.MethodGet, "/", nil)
	r1.Header.Set("x-api-key", "key-a")
	if id, ok := store.AuthenticateAnthropic(r1); !ok || id != "agent-a" {
		t.Fatalf("AuthenticateAnthropic() = %q,%v, want agent-a,true", id, ok)
	}

	r2, _ := http.NewRequest(http.MethodGet, "/", nil)
	if _, ok := store.AuthenticateAnthropic(r2); ok {
		t.Fatal("AuthenticateAnthropic() should fail without header")
	}

	r3, _ := http.NewRequest(http.MethodGet, "/", nil)
	r3.Header.Set("x-api-key", "invalid")
	if _, ok := store.AuthenticateAnthropic(r3); ok {
		t.Fatal("AuthenticateAnthropic() should fail for unknown key")
	}
}
