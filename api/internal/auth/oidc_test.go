package auth

import (
	"context"
	"testing"
)

func TestNewOIDCClientDisabled(t *testing.T) {
	client, err := NewOIDCClient(context.Background(), OIDCConfig{Enabled: false})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if client != nil {
		t.Fatal("expected nil client when disabled")
	}
}

func TestNewOIDCClientMissingConfig(t *testing.T) {
	_, err := NewOIDCClient(context.Background(), OIDCConfig{
		Enabled: true,
		Issuer:  "",
	})
	if err == nil {
		t.Fatal("expected error with missing issuer")
	}
}

func TestGenerateState(t *testing.T) {
	s1, err := GenerateState()
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if s1 == "" {
		t.Fatal("empty state")
	}
	s2, err := GenerateState()
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if s1 == s2 {
		t.Fatal("states should be unique")
	}
}
