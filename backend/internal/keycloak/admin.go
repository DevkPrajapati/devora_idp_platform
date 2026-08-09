// Package keycloak talks to the Keycloak Admin REST API so the platform can
// provision login accounts when an admin adds a project member.
package keycloak

import (
	"bytes"
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

// AdminConfig configures the service-account client used for Admin API calls.
type AdminConfig struct {
	// BaseURL is the Keycloak host, e.g. http://localhost:8080 (no /realms/…).
	BaseURL string
	Realm   string
	// ClientID / ClientSecret identify the confidential client whose service
	// account holds manage-users (typically idp-backend).
	ClientID     string
	ClientSecret string
	HTTPClient   *http.Client
}

// Admin provisions users and assigns realm roles.
type Admin struct {
	cfg    AdminConfig
	client *http.Client

	mu          sync.Mutex
	accessToken string
	tokenExpiry time.Time
}

// NewAdmin returns an Admin client. A zero ClientID disables provisioning
// (EnsureUserWithRole becomes a no-op error the caller can surface).
func NewAdmin(cfg AdminConfig) *Admin {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 15 * time.Second}
	}
	if cfg.Realm == "" {
		cfg.Realm = "idp"
	}
	return &Admin{cfg: cfg, client: cfg.HTTPClient}
}

// Enabled reports whether Admin API credentials are configured.
func (a *Admin) Enabled() bool {
	return a != nil && strings.TrimSpace(a.cfg.BaseURL) != "" &&
		strings.TrimSpace(a.cfg.ClientID) != "" &&
		strings.TrimSpace(a.cfg.ClientSecret) != ""
}

// EnsureUserInput describes the login account to create or update.
type EnsureUserInput struct {
	Email    string
	Username string
	// Password is required when the user does not yet exist. When the user
	// already exists an empty password leaves the current one unchanged.
	Password string
	// Temporary forces a password change on next login when true.
	Temporary bool
	// RealmRole is admin | developer | viewer.
	RealmRole string
}

// EnsureUserWithRole creates the Keycloak user if missing, optionally sets the
// password, and assigns the platform realm role. Idempotent for an existing
// user that already has the role.
func (a *Admin) EnsureUserWithRole(ctx context.Context, in EnsureUserInput) (userID string, created bool, err error) {
	if !a.Enabled() {
		return "", false, fmt.Errorf("keycloak admin is not configured")
	}
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if email == "" {
		return "", false, fmt.Errorf("email is required")
	}
	username := strings.TrimSpace(in.Username)
	if username == "" {
		username = email
	}
	role := strings.ToLower(strings.TrimSpace(in.RealmRole))
	if role == "" {
		return "", false, fmt.Errorf("realm role is required")
	}

	existing, err := a.findUserByEmail(ctx, email)
	if err != nil {
		return "", false, err
	}
	if existing == nil {
		// Also try username match — Keycloak may store email separately.
		existing, err = a.findUserByUsername(ctx, username)
		if err != nil {
			return "", false, err
		}
	}

	if existing == nil {
		if strings.TrimSpace(in.Password) == "" {
			return "", false, fmt.Errorf("password is required to create a new Keycloak login for %s", email)
		}
		userID, err = a.createUser(ctx, username, email, in.Password, in.Temporary)
		if err != nil {
			return "", false, err
		}
		created = true
	} else {
		userID = existing.ID
		if err := a.clearRequiredActions(ctx, userID, email, existing.Username); err != nil {
			return "", false, err
		}
		if strings.TrimSpace(in.Password) != "" {
			if err := a.resetPassword(ctx, userID, in.Password, in.Temporary); err != nil {
				return "", false, err
			}
		}
	}

	if err := a.assignRealmRole(ctx, userID, role); err != nil {
		return "", false, err
	}
	return userID, created, nil
}

type kcUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Enabled  bool   `json:"enabled"`
}

type kcRole struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (a *Admin) findUserByEmail(ctx context.Context, email string) (*kcUser, error) {
	users, err := a.searchUsers(ctx, url.Values{"email": {email}, "exact": {"true"}})
	if err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return nil, nil
	}
	return &users[0], nil
}

func (a *Admin) findUserByUsername(ctx context.Context, username string) (*kcUser, error) {
	users, err := a.searchUsers(ctx, url.Values{"username": {username}, "exact": {"true"}})
	if err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return nil, nil
	}
	return &users[0], nil
}

func (a *Admin) searchUsers(ctx context.Context, q url.Values) ([]kcUser, error) {
	path := fmt.Sprintf("/admin/realms/%s/users?%s", url.PathEscape(a.cfg.Realm), q.Encode())
	var users []kcUser
	if err := a.doJSON(ctx, http.MethodGet, path, nil, &users); err != nil {
		return nil, err
	}
	return users, nil
}

func (a *Admin) createUser(ctx context.Context, username, email, password string, temporary bool) (string, error) {
	first, last := displayNames(username, email)
	body := map[string]any{
		"username":      username,
		"email":         email,
		"firstName":     first,
		"lastName":      last,
		"enabled":       true,
		"emailVerified": true,
		// Empty requiredActions is deliberate: VERIFY_EMAIL / UPDATE_PASSWORD
		// left pending makes password-grant return "Account is not fully set up".
		// firstName/lastName satisfy VERIFY_PROFILE on modern Keycloak.
		"requiredActions": []string{},
		"credentials": []map[string]any{
			{
				"type":      "password",
				"value":     password,
				"temporary": temporary,
			},
		},
	}
	resp, err := a.do(ctx, http.MethodPost, fmt.Sprintf("/admin/realms/%s/users", url.PathEscape(a.cfg.Realm)), body)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusNoContent {
		return "", a.readAPIError(resp, "create user")
	}
	loc := resp.Header.Get("Location")
	var userID string
	if loc == "" {
		// Fall back to a search when Keycloak omits Location.
		u, err := a.findUserByEmail(ctx, email)
		if err != nil {
			return "", err
		}
		if u == nil {
			return "", fmt.Errorf("user created but could not be looked up")
		}
		userID = u.ID
	} else {
		parts := strings.Split(strings.TrimRight(loc, "/"), "/")
		userID = parts[len(parts)-1]
	}
	if err := a.clearRequiredActions(ctx, userID, email, username); err != nil {
		return "", err
	}
	return userID, nil
}

// clearRequiredActions removes VERIFY_EMAIL / UPDATE_PASSWORD / VERIFY_PROFILE
// blockers so the new account can use the password grant immediately.
func (a *Admin) clearRequiredActions(ctx context.Context, userID, email, username string) error {
	first, last := displayNames(username, email)
	body := map[string]any{
		"username":        username,
		"email":           email,
		"firstName":       first,
		"lastName":        last,
		"enabled":         true,
		"emailVerified":   true,
		"requiredActions": []string{},
	}
	path := fmt.Sprintf("/admin/realms/%s/users/%s", url.PathEscape(a.cfg.Realm), url.PathEscape(userID))
	resp, err := a.do(ctx, http.MethodPut, path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return a.readAPIError(resp, "clear required actions")
	}
	return nil
}

func (a *Admin) resetPassword(ctx context.Context, userID, password string, temporary bool) error {
	body := map[string]any{
		"type":      "password",
		"value":     password,
		"temporary": temporary,
	}
	path := fmt.Sprintf("/admin/realms/%s/users/%s/reset-password", url.PathEscape(a.cfg.Realm), url.PathEscape(userID))
	resp, err := a.do(ctx, http.MethodPut, path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return a.readAPIError(resp, "reset password")
	}
	return nil
}

func (a *Admin) assignRealmRole(ctx context.Context, userID, roleName string) error {
	var role kcRole
	path := fmt.Sprintf("/admin/realms/%s/roles/%s", url.PathEscape(a.cfg.Realm), url.PathEscape(roleName))
	if err := a.doJSON(ctx, http.MethodGet, path, nil, &role); err != nil {
		return fmt.Errorf("lookup realm role %q: %w", roleName, err)
	}
	assignPath := fmt.Sprintf("/admin/realms/%s/users/%s/role-mappings/realm",
		url.PathEscape(a.cfg.Realm), url.PathEscape(userID))
	resp, err := a.do(ctx, http.MethodPost, assignPath, []kcRole{role})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return a.readAPIError(resp, "assign realm role")
	}
	return nil
}

func (a *Admin) doJSON(ctx context.Context, method, path string, body any, out any) error {
	resp, err := a.do(ctx, method, path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return a.readAPIError(resp, method+" "+path)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (a *Admin) do(ctx context.Context, method, path string, body any) (*http.Response, error) {
	token, err := a.token(ctx)
	if err != nil {
		return nil, err
	}
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(a.cfg.BaseURL, "/")+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return a.client.Do(req)
}

func (a *Admin) token(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.accessToken != "" && time.Now().Before(a.tokenExpiry.Add(-30*time.Second)) {
		return a.accessToken, nil
	}

	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {a.cfg.ClientID},
		"client_secret": {a.cfg.ClientSecret},
	}
	tokenURL := fmt.Sprintf("%s/realms/%s/protocol/openid-connect/token",
		strings.TrimRight(a.cfg.BaseURL, "/"), url.PathEscape(a.cfg.Realm))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := a.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("keycloak token: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", a.readAPIError(resp, "client credentials token")
	}
	var tok struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return "", err
	}
	if tok.AccessToken == "" {
		return "", fmt.Errorf("keycloak token response missing access_token")
	}
	a.accessToken = tok.AccessToken
	exp := tok.ExpiresIn
	if exp <= 0 {
		exp = 60
	}
	a.tokenExpiry = time.Now().Add(time.Duration(exp) * time.Second)
	return a.accessToken, nil
}

func (a *Admin) readAPIError(resp *http.Response, op string) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	msg := strings.TrimSpace(string(b))
	if msg == "" {
		msg = resp.Status
	}
	return fmt.Errorf("keycloak %s: %s", op, msg)
}

func displayNames(username, email string) (first, last string) {
	base := strings.TrimSpace(username)
	if base == "" {
		base = strings.Split(email, "@")[0]
	}
	if i := strings.IndexAny(base, "._-"); i > 0 {
		return base[:i], base[i+1:]
	}
	return base, "User"
}

// ParseIssuer splits AUTH_ISSUER (http://host/realms/idp) into base URL + realm.
func ParseIssuer(issuer string) (baseURL, realm string) {
	issuer = strings.TrimRight(strings.TrimSpace(issuer), "/")
	const marker = "/realms/"
	i := strings.LastIndex(issuer, marker)
	if i < 0 {
		return issuer, "idp"
	}
	return issuer[:i], issuer[i+len(marker):]
}
