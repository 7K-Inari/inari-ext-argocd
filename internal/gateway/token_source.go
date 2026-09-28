package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ClientCredentialsSource acquires and caches client_credentials JWTs for
// the extension's dedicated Keycloak client (ADR-0008 tunnel auth): the
// control plane's Agent Gateway requires an Authorization: Bearer token
// whose azp resolves the extension registry row. Tokens are cached until
// shortly before expiry; the secret is never logged.
type ClientCredentialsSource struct {
	tokenURL     string
	clientID     string
	clientSecret string
	httpClient   *http.Client

	mu     sync.Mutex
	token  string
	expiry time.Time
}

// NewClientCredentialsSource builds a source from the token endpoint URL
// (e.g. https://keycloak/realms/inari/protocol/openid-connect/token) and
// the extension's dedicated client credentials.
func NewClientCredentialsSource(tokenURL, clientID, clientSecret string) *ClientCredentialsSource {
	return &ClientCredentialsSource{
		tokenURL:     tokenURL,
		clientID:     clientID,
		clientSecret: clientSecret,
		httpClient:   &http.Client{Timeout: 10 * time.Second},
	}
}

// Token returns a cached or freshly-fetched access token.
func (s *ClientCredentialsSource) Token(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token != "" && time.Now().Add(60*time.Second).Before(s.expiry) {
		return s.token, nil
	}
	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {s.clientID},
		"client_secret": {s.clientSecret},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := s.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("gateway: client credentials token request: %w", err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("gateway: read token response: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("gateway: client credentials token: status %d", res.StatusCode)
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.AccessToken == "" {
		return "", fmt.Errorf("gateway: malformed token response")
	}
	exp := 60
	if out.ExpiresIn > 0 {
		exp = out.ExpiresIn
	}
	s.token = out.AccessToken
	s.expiry = time.Now().Add(time.Duration(exp) * time.Second)
	return s.token, nil
}
