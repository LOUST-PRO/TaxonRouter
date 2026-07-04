// Package github provides the shared GraphQL client for TaxonRouter.
// It supports two transports: GitHub App JWT (direct HTTP) and gh CLI
// subprocess fallback. Both satisfy the GraphQLTransport interface so
// callers in internal/mcp don't need to know which is active.
package github

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Client is the GitHub API client. It is safe for concurrent use.
type Client struct {
	transport     GraphQLTransport
	projectID     string
	projectNumber int
	httpClient    *http.Client
	cache         *fieldCache
}

// GraphQLTransport is the interface satisfied by both the JWT HTTP
// transport and the gh subprocess transport.
type GraphQLTransport interface {
	Query(ctx context.Context, query string, vars map[string]any) ([]byte, error)
}

// NewClient returns a Client. If app credentials are present it uses JWT
// auth; otherwise it falls back to gh subprocess.
func NewClient(appID, installationID, privateKeyFile, projectID string, projectNumber int) (*Client, error) {
	var transport GraphQLTransport
	var err error

	// Try GitHub App credentials first.
	if privateKeyFile != "" && appID != "" {
		transport, err = newAppTransport(appID, installationID, privateKeyFile)
		if err != nil {
			// Not a fatal error — fall back to gh.
			transport = newGhTransport()
		}
	} else {
		transport = newGhTransport()
	}

	return &Client{
		transport:     transport,
		projectID:     projectID,
		projectNumber: projectNumber,
		httpClient:    &http.Client{Timeout: 30 * time.Second},
		cache:         newFieldCache(5 * time.Minute),
	}, nil
}

// Query executes a GraphQL query via the active transport.
func (c *Client) Query(ctx context.Context, query string, vars map[string]any) ([]byte, error) {
	return c.transport.Query(ctx, query, vars)
}

// ProjectID returns the configured GitHub Project ID.
func (c *Client) ProjectID() string {
	return c.projectID
}

// GetCachedFieldOptions returns cached field options if fresh.
func (c *Client) GetCachedFieldOptions(key string) ([]FieldOption, bool) {
	if c.cache == nil {
		return nil, false
	}
	return c.cache.Get(key)
}

// CacheFieldOptions stores field options in the cache.
func (c *Client) CacheFieldOptions(key string, options []FieldOption) {
	if c.cache != nil {
		c.cache.Set(key, options)
	}
}

// ResolveOptionID looks up the option ID for a given field and option name.
// It queries GitHub if not cached.
func (c *Client) ResolveOptionID(ctx context.Context, cacheKey, fieldName, optionName string) string {
	// Try cache first.
	if opts, ok := c.GetCachedFieldOptions(cacheKey); ok {
		for _, o := range opts {
			if o.Name == optionName {
				return o.ID
			}
		}
	}
	// Cache miss — caller should re-fetch field_options.
	return optionName
}

// ─────────────────────────────────────────────────────────────────────────
// GitHub App JWT transport
// ─────────────────────────────────────────────────────────────────────────

type appTransport struct {
	appID          string
	installationID string
	privateKey     *rsa.PrivateKey
	httpClient     *http.Client
	baseURL        string
	tokenCache     struct {
		token   string
		expires time.Time
		mu      sync.Mutex
	}
}

func newAppTransport(appID, installationID, privateKeyFile string) (*appTransport, error) {
	keyData, err := os.ReadFile(privateKeyFile)
	if err != nil {
		return nil, fmt.Errorf("read private key: %w", err)
	}
	key, err := jwt.ParseRSAPrivateKeyFromPEM(keyData)
	if err != nil {
		return nil, fmt.Errorf("parse RSA private key: %w", err)
	}
	return &appTransport{
		appID:          appID,
		installationID: installationID,
		privateKey:     key,
		httpClient:     &http.Client{Timeout: 30 * time.Second},
		baseURL:        "https://api.github.com/graphql",
	}, nil
}

func (t *appTransport) Query(ctx context.Context, query string, vars map[string]any) ([]byte, error) {
	token, err := t.token(ctx)
	if err != nil {
		return nil, fmt.Errorf("app token: %w", err)
	}

	body, err := json.Marshal(map[string]any{
		"query":     query,
		"variables": vars,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal query: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.httpClient.Do(req)
	if err != nil {
		// Hot fallback: try gh subprocess before returning.
		fb, fbErr := runGhGraphQL(ctx, query, vars)
		if fbErr != nil {
			return nil, fmt.Errorf("HTTP GraphQL: %w; gh fallback also failed: %w", err, fbErr)
		}
		return fb, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		// If the token expired (401/403), try gh fallback.
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			fb, fbErr := runGhGraphQL(ctx, query, vars)
			if fbErr != nil {
				return nil, fmt.Errorf("GraphQL status %d: %s; gh fallback also failed: %w", resp.StatusCode, string(snippet), fbErr)
			}
			return fb, nil
		}
		return nil, fmt.Errorf("GraphQL status %d: %s", resp.StatusCode, string(snippet))
	}

	return io.ReadAll(resp.Body)
}

func (t *appTransport) token(ctx context.Context) (string, error) {
	t.tokenCache.mu.Lock()
	defer t.tokenCache.mu.Unlock()

	// Use cached token if still valid with 60s buffer.
	if t.tokenCache.token != "" && time.Until(t.tokenCache.expires) > 60*time.Second {
		return t.tokenCache.token, nil
	}

	// Generate JWT.
	now := time.Now()
	claims := jwt.MapClaims{
		"iss": t.appID,
		"iat": jwt.NewNumericDate(now),
		"exp": jwt.NewNumericDate(now.Add(9 * time.Minute)),
	}
	jwtToken := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signed, err := jwtToken.SignedString(t.privateKey)
	if err != nil {
		return "", fmt.Errorf("sign JWT: %w", err)
	}

	// Exchange JWT for installation token.
	reqBody := strings.NewReader(fmt.Sprintf(`{"installation_id":%s}`, t.installationID))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.github.com/app/installations/"+t.installationID+"/access_tokens",
		reqBody)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+signed)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch installation token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return "", fmt.Errorf("installation token status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode installation token: %w", err)
	}

	t.tokenCache.token = result.Token
	t.tokenCache.expires = result.ExpiresAt
	return result.Token, nil
}

// ─────────────────────────────────────────────────────────────────────────
// gh subprocess transport (hot fallback)
// ─────────────────────────────────────────────────────────────────────────

type ghTransport struct{}

func newGhTransport() *ghTransport { return &ghTransport{} }

func (t *ghTransport) Query(ctx context.Context, query string, vars map[string]any) ([]byte, error) {
	return runGhGraphQL(ctx, query, vars)
}

func runGhGraphQL(ctx context.Context, query string, vars map[string]any) ([]byte, error) {
	args := []string{"api", "graphql", "--field", "query=" + query}
	for k, v := range vars {
		args = append(args, "--field", fmt.Sprintf("%s=%v", k, v))
	}

	cmd := exec.CommandContext(ctx, "gh", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("gh graphql: %w (stderr: %s)", err, stderr.String())
	}
	return stdout.Bytes(), nil
}

// ─────────────────────────────────────────────────────────────────────────
// Field cache
// ─────────────────────────────────────────────────────────────────────────

type fieldCache struct {
	ttl   time.Duration
	items map[string]cachedField
	mu    sync.RWMutex
}

type cachedField struct {
	options []FieldOption
	at     time.Time
}

func newFieldCache(ttl time.Duration) *fieldCache {
	return &fieldCache{ttl: ttl, items: make(map[string]cachedField)}
}

func (c *fieldCache) Get(key string) ([]FieldOption, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	f, ok := c.items[key]
	if !ok {
		return nil, false
	}
	if time.Since(f.at) > c.ttl {
		return nil, false
	}
	return f.options, true
}

func (c *fieldCache) Set(key string, options []FieldOption) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[key] = cachedField{options: options, at: time.Now()}
}

func (c *fieldCache) Invalidate(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, key)
}

// FieldOption is one option of a SINGLE_SELECT field.
type FieldOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// IsLikelyOptionID returns true if s looks like a GitHub global relay ID
// (base64-encoded, starts with "I_"). Human-readable names are not IDs.
func IsLikelyOptionID(s string) bool {
	return strings.HasPrefix(s, "I_") || (len(s) == 24 && strings.IndexByte(s, '_') > 0)
}
