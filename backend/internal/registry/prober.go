package registry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/idp/platform/backend/internal/kubernetes"
)

// probeTimeout bounds the whole Test Connection round trip. Registries that
// hang would otherwise hold a request goroutine for as long as the client waits.
const probeTimeout = 10 * time.Second

// maxProbeBody caps how much of a registry's response is read. Error bodies are
// small; anything larger is either hostile or useless.
const maxProbeBody = 32 << 10

// dockerHubAPIHost is where Docker Hub's v2 API actually lives. Its credential
// key is the legacy v1 index URL, which has no v2 endpoint to probe.
const dockerHubAPIHost = "registry-1.docker.io"

// HTTPProber authenticates against a Docker Registry v2 endpoint.
//
// The v2 spec has no "check my password" call, so this performs the standard
// discovery handshake: GET /v2/ and follow whatever the registry's
// WWW-Authenticate header asks for. That is the same path the kubelet takes,
// so a green result here means image pulls will actually work.
type HTTPProber struct {
	client *http.Client
}

// NewHTTPProber creates a prober with SSRF guards in place.
func NewHTTPProber() *HTTPProber {
	return &HTTPProber{
		client: &http.Client{
			Timeout: probeTimeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				// Registries redirect to CDNs for blobs, but never during the
				// auth handshake. Re-checking each hop keeps a hostile registry
				// from bouncing us at an internal address with the
				// Authorization header still attached.
				if len(via) >= 3 {
					return errors.New("too many redirects")
				}
				return guardAddress(req.Context(), req.URL.Hostname())
			},
		},
	}
}

// guardAddress rejects destinations that only make sense as an SSRF target.
// Cloud instance metadata (169.254.169.254) is the sharpest edge: reaching it
// from inside the backend can hand out the platform's own cloud credentials.
// RFC 1918 addresses stay allowed — self-hosted registries legitimately live
// on private networks, and blocking them would break the main use case.
func guardAddress(ctx context.Context, host string) error {
	if host == "" {
		return errors.New("registry host is empty")
	}

	// Resolved through the context-aware resolver so a hostile or slow
	// authoritative server cannot pin the goroutine open past the caller's
	// deadline. net.LookupIP ignores cancellation entirely.
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("cannot resolve %s", host)
	}
	for _, addr := range addrs {
		ip := addr.IP
		switch {
		case ip.IsLoopback():
			return fmt.Errorf("refusing to probe loopback address for %s", host)
		case ip.IsLinkLocalUnicast(), ip.IsLinkLocalMulticast():
			return fmt.Errorf("refusing to probe link-local address for %s", host)
		case ip.IsUnspecified():
			return fmt.Errorf("refusing to probe unspecified address for %s", host)
		}
	}
	return nil
}

// Probe reports nil when the credentials authenticate against the registry.
// Returned errors are shown verbatim to the user, so they describe the problem
// without echoing the password.
func (p *HTTPProber) Probe(ctx context.Context, registryURL, username, password string) error {
	host, err := kubernetes.NormalizeRegistryHost(registryURL)
	if err != nil {
		return err
	}

	apiHost := host
	if host == "https://index.docker.io/v1/" {
		apiHost = dockerHubAPIHost
	}

	scheme := "https"
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(registryURL)), "http://") {
		scheme = "http"
	}
	if err := guardAddress(ctx, hostWithoutPort(apiHost)); err != nil {
		return err
	}

	base := fmt.Sprintf("%s://%s/v2/", scheme, apiHost)

	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	resp, err := p.get(ctx, base, "")
	if err != nil {
		return fmt.Errorf("cannot reach %s: %w", apiHost, err)
	}
	defer drain(resp)

	switch {
	case resp.StatusCode == http.StatusOK:
		// An open registry. Credentials are unused but valid by definition.
		return nil

	case resp.StatusCode == http.StatusUnauthorized:
		challenge := resp.Header.Get("WWW-Authenticate")
		if strings.HasPrefix(strings.ToLower(challenge), "bearer ") {
			return p.probeBearer(ctx, challenge, username, password)
		}
		return p.probeBasic(ctx, base, username, password)

	case resp.StatusCode == http.StatusNotFound:
		return fmt.Errorf("%s does not expose a Docker Registry v2 API", apiHost)

	default:
		return fmt.Errorf("registry returned HTTP %d", resp.StatusCode)
	}
}

// probeBasic retries the endpoint with HTTP basic auth, the scheme used by
// Harbor, Nexus, and most self-hosted registries.
func (p *HTTPProber) probeBasic(ctx context.Context, endpoint, username, password string) error {
	resp, err := p.get(ctx, endpoint, basicAuthHeader(username, password))
	if err != nil {
		return fmt.Errorf("authentication request failed: %w", err)
	}
	defer drain(resp)

	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return errors.New("registry rejected the username or password")
	default:
		return fmt.Errorf("registry returned HTTP %d during authentication", resp.StatusCode)
	}
}

// probeBearer performs the token exchange described by the registry's
// WWW-Authenticate challenge. This is the path Docker Hub, GHCR, GCR and ECR
// all take: the token endpoint is where credentials are actually verified.
func (p *HTTPProber) probeBearer(ctx context.Context, challenge, username, password string) error {
	params := parseChallenge(challenge)
	realm := params["realm"]
	if realm == "" {
		return errors.New("registry sent a bearer challenge without a realm")
	}

	tokenURL, err := url.Parse(realm)
	if err != nil {
		return fmt.Errorf("registry sent an unusable auth realm: %w", err)
	}
	if tokenURL.Scheme != "https" && tokenURL.Scheme != "http" {
		return fmt.Errorf("unsupported auth realm scheme %q", tokenURL.Scheme)
	}
	if err := guardAddress(ctx, tokenURL.Hostname()); err != nil {
		return err
	}

	query := tokenURL.Query()
	if service := params["service"]; service != "" {
		query.Set("service", service)
	}
	// Scope is optional; omitting it asks for a bare login token, which is
	// exactly what a credential check wants.
	tokenURL.RawQuery = query.Encode()

	resp, err := p.get(ctx, tokenURL.String(), basicAuthHeader(username, password))
	if err != nil {
		return fmt.Errorf("token request failed: %w", err)
	}
	defer drain(resp)

	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return errors.New("registry rejected the username or password")
	default:
		return fmt.Errorf("token endpoint returned HTTP %d", resp.StatusCode)
	}
}

func (p *HTTPProber) get(ctx context.Context, endpoint, authorization string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "idp-platform/registry-probe")
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	return p.client.Do(req)
}

// parseChallenge splits `Bearer realm="x",service="y"` into its parameters.
func parseChallenge(challenge string) map[string]string {
	params := make(map[string]string)
	_, rest, found := strings.Cut(challenge, " ")
	if !found {
		return params
	}
	for _, part := range strings.Split(rest, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		params[strings.ToLower(strings.TrimSpace(key))] = strings.Trim(strings.TrimSpace(value), `"`)
	}
	return params
}

func basicAuthHeader(username, password string) string {
	req := &http.Request{Header: http.Header{}}
	req.SetBasicAuth(username, password)
	return req.Header.Get("Authorization")
}

func hostWithoutPort(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}

// drain consumes and closes a response so the connection can be reused, while
// capping how much of a hostile body is read.
func drain(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxProbeBody))
	_ = resp.Body.Close()
}
