package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// OIDCConfig holds the OIDC provider settings from environment.
// Note: no RedirectURL — it's derived per-request from the incoming HTTP
// request (scheme + host + callback path). Register the resulting URL
// (e.g. https://your-host/api/auth/oidc/callback) in your IdP.
type OIDCConfig struct {
	Enabled      bool
	Issuer       string
	ClientID     string
	ClientSecret string
}

// OIDCClient wraps the OIDC provider and OAuth2 config.
type OIDCClient struct {
	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
	oauth2   oauth2.Config
}

// NewOIDCClient creates an OIDC client from config. Returns nil if not enabled.
func NewOIDCClient(ctx context.Context, cfg OIDCConfig) (*OIDCClient, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	if cfg.Issuer == "" || cfg.ClientID == "" {
		return nil, fmt.Errorf("oidc: OIDC_ISSUER and OIDC_CLIENT_ID are required")
	}

	provider, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc: failed to get provider: %w", err)
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: cfg.ClientID})

	oauth2Config := oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}

	return &OIDCClient{
		provider: provider,
		verifier: verifier,
		oauth2:   oauth2Config,
	}, nil
}

// AuthCodeURL returns the URL to redirect the user to for authentication.
// The redirectURL is derived per-request so no static configuration is needed.
func (c *OIDCClient) AuthCodeURL(state, redirectURL string) string {
	cfg := c.oauth2
	cfg.RedirectURL = redirectURL
	return cfg.AuthCodeURL(state, oauth2.AccessTypeOnline)
}

// Exchange exchanges the authorization code for tokens and verifies the ID token.
// Returns the user's email and name from the claims.
func (c *OIDCClient) Exchange(ctx context.Context, code string) (email, name string, err error) {
	token, err := c.oauth2.Exchange(ctx, code)
	if err != nil {
		return "", "", fmt.Errorf("oidc: code exchange failed: %w", err)
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		return "", "", fmt.Errorf("oidc: no id_token in response")
	}

	idToken, err := c.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return "", "", fmt.Errorf("oidc: failed to verify id token: %w", err)
	}

	var claims struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return "", "", fmt.Errorf("oidc: failed to parse claims: %w", err)
	}

	return claims.Email, claims.Name, nil
}

// GenerateState creates a random state parameter for CSRF protection.
func GenerateState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// StateExpiry is how long an OIDC state is valid.
const StateExpiry = 10 * time.Minute
