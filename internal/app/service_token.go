package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/biairmal/go-service-template/internal/platform/clock"
)

// serviceTokenMargin is how long before expiry a cached service token is
// replaced, so a token never expires between the check and identity's use.
const serviceTokenMargin = 30 * time.Second

// serviceToken trades this service's client credentials (identity.client_id
// and client_secret) at identity's POST /v1/auth/token for a service token,
// and caches it until shortly before it expires. authz calls Token only when
// it refreshes a tenant's role map.
type serviceToken struct {
	url    string
	id     string
	secret string
	client *http.Client
	clock  clock.Clock

	mu      sync.Mutex
	token   string
	refresh time.Time // when the cached token must be replaced
}

func newServiceToken(identityURL string, cfg IdentityConfig, client *http.Client, c clock.Clock) *serviceToken {
	return &serviceToken{
		url: strings.TrimRight(identityURL, "/") + "/v1/auth/token", id: cfg.ClientID, secret: cfg.ClientSecret,
		client: client, clock: c,
	}
}

// Token returns the cached service token, or fetches a new one when it is
// missing or about to expire.
func (s *serviceToken) Token(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token != "" && s.clock.Now().Before(s.refresh) {
		return s.token, nil
	}
	token, ttl, err := s.fetch(ctx)
	if err != nil {
		return "", err
	}
	s.token, s.refresh = token, s.clock.Now().Add(ttl-serviceTokenMargin)
	return s.token, nil
}

// fetch runs the client-credentials exchange.
func (s *serviceToken) fetch(ctx context.Context) (token string, ttl time.Duration, err error) {
	body, err := json.Marshal(map[string]string{
		"grant_type": "client_credentials", "client_id": s.id, "client_secret": s.secret,
	})
	if err != nil {
		return "", 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(body))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	var env struct {
		Data struct {
			AccessToken string `json:"access_token"`
			ExpiresIn   int    `json:"expires_in"`
		} `json:"data"`
	}
	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("service token: identity returned %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return "", 0, fmt.Errorf("service token: %w", err)
	}
	if env.Data.AccessToken == "" {
		return "", 0, fmt.Errorf("service token: identity returned no access_token")
	}
	return env.Data.AccessToken, time.Duration(env.Data.ExpiresIn) * time.Second, nil
}
