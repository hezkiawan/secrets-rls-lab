// Package openbao wraps the official OpenBao Go client for our API:
// logging in with AppRole and reading this product's secrets.
package openbao

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/openbao/openbao/api/auth/approle/v2"
	bao "github.com/openbao/openbao/api/v2"
)

// Client is our small wrapper around the official client.
type Client struct {
	api     *bao.Client
	kvMount string

	// remembered so the client can log in again when its token can't be renewed (M5)
	roleIDFile, secretIDFile string
	authSecret               *bao.Secret
}

// NewClient creates an (unauthenticated) client for the given address.
func NewClient(addr, kvMount string) (*Client, error) {
	cfg := bao.DefaultConfig() // also reads BAO_* env vars (and VAULT_* as fallback)
	cfg.Address = addr
	cfg.Timeout = 5 * time.Second

	c, err := bao.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("creating OpenBao client: %w", err)
	}
	return &Client{api: c, kvMount: kvMount}, nil
}

// LoginAppRole proves the API's identity with AppRole:
//   - role ID   = which role ("username"), read from roleIDFile
//   - secret ID = proof          ("password"), read from secretIDFile
//
// On success OpenBao returns a token; the client library stores it, and every
// later request uses it. The token carries this role's policies — nothing more.
func (c *Client) LoginAppRole(ctx context.Context, roleIDFile, secretIDFile string) error {
	roleIDBytes, err := os.ReadFile(roleIDFile)
	if err != nil {
		return fmt.Errorf("reading role ID file %q: %w", roleIDFile, err)
	}
	roleID := strings.TrimSpace(string(roleIDBytes))

	auth, err := approle.NewAppRoleAuth(roleID, &approle.SecretID{FromFile: secretIDFile})
	if err != nil {
		return fmt.Errorf("preparing AppRole login: %w", err)
	}

	secret, err := c.api.Auth().Login(ctx, auth)
	if err != nil {
		return fmt.Errorf("AppRole login failed: %w", err)
	}
	if secret == nil || secret.Auth == nil {
		return errors.New("AppRole login returned no token")
	}
	c.roleIDFile, c.secretIDFile, c.authSecret = roleIDFile, secretIDFile, secret
	return nil
}

// TokenInfo describes the token we're currently using (never the token itself).
type TokenInfo struct {
	Policies     []string `json:"policies"`
	TTLRemaining string   `json:"ttl_remaining"`
}

// LookupSelf asks OpenBao "what is my current token allowed to do, and for how long?"
func (c *Client) LookupSelf(ctx context.Context) (*TokenInfo, error) {
	secret, err := c.api.Auth().Token().LookupSelfWithContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("token lookup: %w", err)
	}
	policies, err := secret.TokenPolicies()
	if err != nil {
		return nil, fmt.Errorf("reading token policies: %w", err)
	}
	ttl, err := secret.TokenTTL()
	if err != nil {
		return nil, fmt.Errorf("reading token TTL: %w", err)
	}
	return &TokenInfo{Policies: policies, TTLRemaining: ttl.String()}, nil
}

// AppSecrets are the secrets this API needs to run.
// Fields are unexported (lowercase) on purpose: other packages must use the
// methods below, and Go's JSON encoder will never print them by accident.
type AppSecrets struct {
	jwtSigningKey          string
	metaAPIToken           string
	firebaseServiceAccount string
}

// JWTSigningKey returns the key used to sign/verify user tokens (needed in M3).
func (s *AppSecrets) JWTSigningKey() string { return s.jwtSigningKey }

// Fingerprints returns a short hash of each secret: enough to prove the right
// value was loaded (and to notice when it changes) without ever revealing it.
func (s *AppSecrets) Fingerprints() map[string]string {
	return map[string]string{
		"jwt_signing_key":          fingerprint(s.jwtSigningKey),
		"meta_api_token":           fingerprint(s.metaAPIToken),
		"firebase_service_account": fingerprint(s.firebaseServiceAccount),
	}
}

// LoadAppSecrets reads <product>/app and <product>/firebase from KV v2.
func (c *Client) LoadAppSecrets(ctx context.Context, product string) (*AppSecrets, error) {
	kv := c.api.KVv2(c.kvMount)

	app, err := kv.Get(ctx, product+"/app")
	if err != nil {
		return nil, fmt.Errorf("reading %s/app: %w", product, err)
	}
	firebase, err := kv.Get(ctx, product+"/firebase")
	if err != nil {
		return nil, fmt.Errorf("reading %s/firebase: %w", product, err)
	}

	s := &AppSecrets{}
	if s.jwtSigningKey, err = stringField(app.Data, "jwt_signing_key"); err != nil {
		return nil, err
	}
	if s.metaAPIToken, err = stringField(app.Data, "meta_api_token"); err != nil {
		return nil, err
	}
	if s.firebaseServiceAccount, err = stringField(firebase.Data, "service_account_json"); err != nil {
		return nil, err
	}
	return s, nil
}

// DBLogin is a database username + password read from OpenBao.
type DBLogin struct {
	Username string
	Password string
}

// LoadDBLogin reads a static database login stored in KV, e.g. "kouventa/db"
// (keys: username, password). M5 replaces this with dynamic credentials.
func (c *Client) LoadDBLogin(ctx context.Context, path string) (*DBLogin, error) {
	s, err := c.api.KVv2(c.kvMount).Get(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var login DBLogin
	if login.Username, err = stringField(s.Data, "username"); err != nil {
		return nil, err
	}
	if login.Password, err = stringField(s.Data, "password"); err != nil {
		return nil, err
	}
	return &login, nil
}

// ReadResult is what the /demo/read endpoint reports.
type ReadResult struct {
	Path    string   `json:"path"`
	Allowed bool     `json:"allowed"`
	Keys    []string `json:"keys,omitempty"` // key NAMES only, never values
	Error   string   `json:"error,omitempty"`
}

// TryRead attempts to read any KV path with the API's own token.
// It exists only to demonstrate the policy: which reads succeed, which are denied.
func (c *Client) TryRead(ctx context.Context, path string) ReadResult {
	result := ReadResult{Path: c.kvMount + "/" + path}

	secret, err := c.api.KVv2(c.kvMount).Get(ctx, path)
	if err != nil {
		var respErr *bao.ResponseError
		if errors.As(err, &respErr) && respErr.StatusCode == http.StatusForbidden {
			result.Error = "403 permission denied — the policy does not allow this path"
		} else {
			result.Error = err.Error()
		}
		return result
	}

	result.Allowed = true
	for key := range secret.Data {
		result.Keys = append(result.Keys, key)
	}
	return result
}

// stringField pulls one string value out of a secret's data map.
// KV data comes back as map[string]any, so we must check the type ourselves.
func stringField(data map[string]any, key string) (string, error) {
	raw, ok := data[key]
	if !ok {
		return "", fmt.Errorf("secret is missing key %q", key)
	}
	value, ok := raw.(string) // "type assertion": is this `any` actually a string?
	if !ok || value == "" {
		return "", fmt.Errorf("secret key %q is not a non-empty string", key)
	}
	return value, nil
}

func fingerprint(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])[:12]
}
