package auth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ValidatorConfig holds JWT validation settings.
type ValidatorConfig struct {
	Issuer   string
	Audience string
	JWKSURL  string
	Enabled  bool
}

// Validator validates JWT tokens against Keycloak JWKS.
type Validator struct {
	cfg        ValidatorConfig
	httpClient *http.Client
	mu         sync.RWMutex
	keys       map[string]*rsa.PublicKey
	lastFetch  time.Time
	cacheTTL   time.Duration
}

// JWKS represents a JSON Web Key Set response.
type JWKS struct {
	Keys []JWK `json:"keys"`
}

// JWK represents a single JSON Web Key.
type JWK struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	N   string `json:"n"`
	E   string `json:"e"`
	Alg string `json:"alg"`
}

// NewValidator creates a JWT validator with JWKS caching.
func NewValidator(cfg ValidatorConfig) *Validator {
	return &Validator{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		keys:       make(map[string]*rsa.PublicKey),
		cacheTTL:   5 * time.Minute,
	}
}

// Enabled reports whether JWT validation is active.
func (v *Validator) Enabled() bool {
	return v.cfg.Enabled
}

// DevUser returns the default user used when auth is disabled.
func DevUser() *User {
	return &User{
		ID:       "dev-user",
		Email:    "dev@idp.local",
		Username: "dev",
		Roles:    []Role{RoleAdmin},
	}
}

// Validate parses and validates a bearer token, returning the authenticated user.
func (v *Validator) Validate(ctx context.Context, tokenString string) (*User, error) {
	if !v.cfg.Enabled {
		return DevUser(), nil
	}

	tokenString = strings.TrimPrefix(tokenString, "Bearer ")
	tokenString = strings.TrimSpace(tokenString)
	if tokenString == "" {
		return nil, fmt.Errorf("missing token")
	}

	if err := v.refreshKeys(ctx); err != nil {
		return nil, fmt.Errorf("refresh jwks: %w", err)
	}

	parser := jwt.NewParser(jwt.WithValidMethods([]string{"RS256"}))
	token, err := parser.Parse(tokenString, func(token *jwt.Token) (any, error) {
		kid, ok := token.Header["kid"].(string)
		if !ok {
			return nil, fmt.Errorf("missing kid in token header")
		}

		v.mu.RLock()
		key, exists := v.keys[kid]
		v.mu.RUnlock()
		if !exists {
			return nil, fmt.Errorf("unknown key id: %s", kid)
		}
		return key, nil
	})
	if err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}

	if err := v.validateClaims(claims); err != nil {
		return nil, err
	}

	return userFromClaims(claims), nil
}

func (v *Validator) validateClaims(claims jwt.MapClaims) error {
	if iss, ok := claims["iss"].(string); !ok || iss != v.cfg.Issuer {
		return fmt.Errorf("invalid issuer")
	}

	if v.cfg.Audience != "" {
		if !audienceMatches(claims, v.cfg.Audience) {
			return fmt.Errorf("invalid audience")
		}
	}

	if exp, err := claims.GetExpirationTime(); err == nil && exp != nil && exp.Before(time.Now()) {
		return fmt.Errorf("token expired")
	}

	return nil
}

func audienceMatches(claims jwt.MapClaims, expected string) bool {
	switch aud := claims["aud"].(type) {
	case string:
		return aud == expected
	case []any:
		for _, item := range aud {
			if s, ok := item.(string); ok && s == expected {
				return true
			}
		}
	}
	return false
}

func userFromClaims(claims jwt.MapClaims) *User {
	user := &User{
		ID:       claimString(claims, "sub"),
		Email:    claimString(claims, "email"),
		Username: claimString(claims, "preferred_username"),
	}

	if user.Email == "" {
		user.Email = claimString(claims, "upn")
	}

	roles := extractRoles(claims)
	user.Roles = roles
	return user
}

func claimString(claims jwt.MapClaims, key string) string {
	if val, ok := claims[key].(string); ok {
		return val
	}
	return ""
}

func extractRoles(claims jwt.MapClaims) []Role {
	roleSet := make(map[Role]bool)

	if realmAccess, ok := claims["realm_access"].(map[string]any); ok {
		if roles, ok := realmAccess["roles"].([]any); ok {
			for _, r := range roles {
				if name, ok := r.(string); ok {
					roleSet[Role(name)] = true
				}
			}
		}
	}

	if resourceAccess, ok := claims["resource_access"].(map[string]any); ok {
		for _, client := range resourceAccess {
			if clientMap, ok := client.(map[string]any); ok {
				if roles, ok := clientMap["roles"].([]any); ok {
					for _, r := range roles {
						if name, ok := r.(string); ok {
							roleSet[Role(name)] = true
						}
					}
				}
			}
		}
	}

	roles := make([]Role, 0, len(roleSet))
	for role := range roleSet {
		roles = append(roles, role)
	}
	return roles
}

func (v *Validator) refreshKeys(ctx context.Context) error {
	v.mu.RLock()
	stale := time.Since(v.lastFetch) > v.cacheTTL || len(v.keys) == 0
	v.mu.RUnlock()
	if !stale {
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.cfg.JWKSURL, nil)
	if err != nil {
		return err
	}

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("jwks request failed: status %d", resp.StatusCode)
	}

	var jwks JWKS
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return fmt.Errorf("decode jwks: %w", err)
	}

	keys := make(map[string]*rsa.PublicKey)
	for _, key := range jwks.Keys {
		if key.Kty != "RSA" || key.Kid == "" {
			continue
		}
		pub, err := jwkToRSAPublicKey(key)
		if err != nil {
			continue
		}
		keys[key.Kid] = pub
	}

	if len(keys) == 0 {
		return fmt.Errorf("no valid keys in jwks")
	}

	v.mu.Lock()
	v.keys = keys
	v.lastFetch = time.Now()
	v.mu.Unlock()

	return nil
}

func jwkToRSAPublicKey(jwk JWK) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(jwk.N)
	if err != nil {
		return nil, err
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(jwk.E)
	if err != nil {
		return nil, err
	}

	n := new(big.Int).SetBytes(nBytes)
	e := 0
	for _, b := range eBytes {
		e = e<<8 + int(b)
	}

	return &rsa.PublicKey{N: n, E: e}, nil
}
