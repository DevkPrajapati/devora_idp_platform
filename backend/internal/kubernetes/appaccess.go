package kubernetes

import (
	"context"
	"fmt"
	"hash/fnv"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

// AppsPathPrefix is the HTTP path under which the platform opens workloads in
// the browser without requiring kubectl or /etc/hosts.
const AppsPathPrefix = "/apps/"

// TicketQueryParam carries the signed authorization for a redemption.
//
// It travels in the query string because this endpoint is reached by a
// top-level browser navigation, which cannot set an Authorization header. The
// ticket is single-workload and expires in under a minute, so the usual
// objection to credentials in URLs — long-lived secrets landing in logs and
// Referer headers — is bounded to a value that is useless by the time anyone
// reads it.
const TicketQueryParam = "ticket"

const (
	// maxSessions caps concurrent port-forwards. Each one holds an open SPDY
	// stream to the API server and a listening local socket, so an unbounded
	// map is a file-descriptor exhaustion vector.
	maxSessions = 64
	// sessionIdleTimeout reclaims a forward nobody has used recently.
	// Kept long enough that a rolling redeploy (usually <2m) does not force
	// the user to open a brand-new localhost port.
	sessionIdleTimeout = 30 * time.Minute
	// reapInterval is how often idle sessions are swept.
	reapInterval = 30 * time.Second
	// stickyPortBase/Count pick a stable loopback port per workload so a
	// redeploy can reconnect without opening a new listener. The browser is
	// never sent to that port — HostHandler proxies *.localhost instead.
	stickyPortBase  = 18000
	stickyPortCount = 1000
)

// TicketVerifier authorizes one app-access redemption.
//
// Declared here rather than imported from the auth package so this package
// stays free of authentication machinery: it needs a yes/no, not a JWT parser.
type TicketVerifier interface {
	// Verify returns the subject the ticket was issued to, or an error if the
	// ticket is missing, expired, forged, or issued for a different workload.
	Verify(ticket, namespace, name string) (string, error)
}

// appForward keeps a localhost port-forward alive for one workload so absolute
// asset paths (Vite, nginx) resolve against the app origin instead of the IDP.
type appForward struct {
	localPort uint16
	stopCh    chan struct{}
	// podName is the pod this forward is attached to. After a rollout the
	// ReplicaSet changes and this pod disappears — ensureForward must rebuild.
	podName   string
	namespace string
	// lastUsed drives idle reaping. Guarded by AppAccess.mu.
	lastUsed time.Time
}

// AppAccess manages click-to-open sessions for deployed workloads.
type AppAccess struct {
	client   *Client
	verifier TicketVerifier
	mu       sync.Mutex
	active   map[string]*appForward
	// hosts maps "{ns}--{name}.localhost" onto a session key so the public
	// hostname can reverse-proxy without exposing 127.0.0.1:18xxx.
	hosts map[string]string
	// stopReaper ends the janitor goroutine on shutdown.
	stopReaper context.CancelFunc
	// now is injectable so tests can advance time without sleeping.
	now func() time.Time
}

// NewAppAccess creates an access manager and starts its idle-session reaper.
//
// Returns nil when Kubernetes is unavailable or no verifier was supplied. A nil
// verifier is treated as "no service" rather than "allow everything": this
// endpoint port-forwards into arbitrary pods, so failing closed is the only
// safe default.
func NewAppAccess(client *Client, verifier TicketVerifier) *AppAccess {
	if client == nil || verifier == nil {
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	a := &AppAccess{
		client:     client,
		verifier:   verifier,
		active:     make(map[string]*appForward),
		hosts:      make(map[string]string),
		stopReaper: cancel,
		now:        time.Now,
	}

	go a.reapLoop(ctx)
	return a
}

func sessionKey(namespace, name string) string {
	return namespace + "/" + name
}

// AppLocalHost is the browser hostname for a workload. Chrome/Firefox resolve
// *.localhost to 127.0.0.1, so no /etc/hosts entry is required.
//
// A double hyphen separates namespace from name because both may already
// contain hyphens (user-auth1 / user-web).
func AppLocalHost(namespace, name string) string {
	return strings.ToLower(strings.TrimSpace(namespace) + "--" + strings.TrimSpace(name) + ".localhost")
}

func requestHostName(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return strings.ToLower(h)
	}
	return strings.ToLower(host)
}

func requestListenPort(host string) string {
	if _, p, err := net.SplitHostPort(host); err == nil && p != "" {
		return p
	}
	return "80"
}

// Handler serves GET /apps/{namespace}/{name}?ticket=... , starts the
// port-forward, and redirects the browser to http://{ns}--{name}.localhost:PORT/
// on the IDP listen port. That host is reverse-proxied by HostHandler, so the
// address bar stays on localhost and never shows 127.0.0.1:18xxx.
//
// Every request must present a ticket minted by the authenticated
// CreateAppAccessTicket RPC, which is where the caller's permission to reach
// this namespace was actually checked.
func (a *AppAccess) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a == nil || !a.client.Available() {
			http.Error(w, "kubernetes cluster not connected", http.StatusServiceUnavailable)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		path := strings.TrimPrefix(r.URL.Path, AppsPathPrefix)
		path = strings.Trim(path, "/")
		parts := strings.Split(path, "/")
		if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
			http.Error(w, "usage: /apps/{namespace}/{name}", http.StatusBadRequest)
			return
		}
		namespace, name := parts[0], parts[1]

		// Authorization precedes every cluster read below. Until this passes,
		// an unauthenticated caller cannot even learn whether the namespace
		// exists — the response is identical either way.
		if _, err := a.verifier.Verify(r.URL.Query().Get(TicketQueryParam), namespace, name); err != nil {
			http.Error(w, "invalid or expired access link", http.StatusForbidden)
			return
		}

		// A leftover forward from the previous image would keep serving old
		// HTML after a successful build. Always attach to the newest Ready pod.
		a.resetForward(namespace, name)
		if _, err := a.ensureForward(r, namespace, name); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}

		a.bindHost(namespace, name)
		listen := requestListenPort(r.Host)
		target := fmt.Sprintf("http://%s/", AppLocalHost(namespace, name))
		if listen != "80" {
			target = fmt.Sprintf("http://%s:%s/", AppLocalHost(namespace, name), listen)
		}
		http.Redirect(w, r, target, http.StatusFound)
	})
}

// HostHandler reverse-proxies {ns}--{name}.localhost onto the local forward
// so the tab stays on a localhost URL. Requests for any other host pass through.
func (a *AppAccess) HostHandler(next http.Handler) http.Handler {
	if a == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := requestHostName(r.Host)
		if host == "" || host == "localhost" || host == "127.0.0.1" || !strings.HasSuffix(host, ".localhost") {
			next.ServeHTTP(w, r)
			return
		}

		a.mu.Lock()
		key, ok := a.hosts[host]
		a.mu.Unlock()
		if !ok {
			http.Error(w, "this app is not open yet — use Open site from the IDP", http.StatusNotFound)
			return
		}

		namespace, name, found := strings.Cut(key, "/")
		if !found {
			http.Error(w, "invalid app session", http.StatusBadGateway)
			return
		}
		port, err := a.ensureForward(r, namespace, name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		a.proxyToForward(w, r, port)
	})
}

func (a *AppAccess) resetForward(namespace, name string) {
	if a == nil {
		return
	}
	key := sessionKey(namespace, name)
	a.mu.Lock()
	fwd, ok := a.active[key]
	a.mu.Unlock()
	if ok {
		a.dropSession(key, fwd.stopCh)
	}
}

func (a *AppAccess) proxyToForward(w http.ResponseWriter, r *http.Request, port uint16) {
	target, err := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.FlushInterval = -1
	proxy.ErrorHandler = func(rw http.ResponseWriter, _ *http.Request, proxyErr error) {
		http.Error(rw, "app port-forward lost: "+proxyErr.Error(), http.StatusBadGateway)
	}
	proxy.ModifyResponse = applyAppProxyCacheHeaders
	proxy.ServeHTTP(w, r)
}

// applyAppProxyCacheHeaders stops the browser from keeping a previous deploy's
// index.html after Open site starts forwarding a new pod.
func applyAppProxyCacheHeaders(resp *http.Response) error {
	if resp == nil || resp.Request == nil {
		return nil
	}
	path := resp.Request.URL.Path
	ct := resp.Header.Get("Content-Type")
	if path != "/" && path != "/index.html" && !strings.HasSuffix(path, "/index.html") && !strings.Contains(ct, "text/html") {
		return nil
	}
	resp.Header.Set("Cache-Control", "no-store, no-cache, must-revalidate")
	resp.Header.Set("Pragma", "no-cache")
	resp.Header.Del("ETag")
	resp.Header.Del("Last-Modified")
	return nil
}

func (a *AppAccess) bindHost(namespace, name string) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.hosts == nil {
		a.hosts = make(map[string]string)
	}
	a.hosts[AppLocalHost(namespace, name)] = sessionKey(namespace, name)
}

func (a *AppAccess) unbindHostsForKeyLocked(key string) {
	for host, mapped := range a.hosts {
		if mapped == key {
			delete(a.hosts, host)
		}
	}
}

// Close stops every active port-forward and the reaper. Safe to call twice.
func (a *AppAccess) Close() {
	if a == nil {
		return
	}
	if a.stopReaper != nil {
		a.stopReaper()
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	for key, fwd := range a.active {
		close(fwd.stopCh)
		delete(a.active, key)
		a.unbindHostsForKeyLocked(key)
	}
}

// reapLoop closes forwards that have gone idle.
func (a *AppAccess) reapLoop(ctx context.Context) {
	ticker := time.NewTicker(reapInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.reapIdle()
		}
	}
}

func (a *AppAccess) reapIdle() {
	cutoff := a.now().Add(-sessionIdleTimeout)

	a.mu.Lock()
	defer a.mu.Unlock()
	for key, fwd := range a.active {
		if fwd.lastUsed.Before(cutoff) {
			close(fwd.stopCh)
			delete(a.active, key)
			a.unbindHostsForKeyLocked(key)
		}
	}
}

// evictOldestLocked frees one slot when the session cap is reached. Callers
// must hold a.mu.
func (a *AppAccess) evictOldestLocked() {
	var oldestKey string
	var oldest time.Time

	for key, fwd := range a.active {
		if oldestKey == "" || fwd.lastUsed.Before(oldest) {
			oldestKey, oldest = key, fwd.lastUsed
		}
	}

	if oldestKey != "" {
		close(a.active[oldestKey].stopCh)
		delete(a.active, oldestKey)
		a.unbindHostsForKeyLocked(oldestKey)
	}
}

func (a *AppAccess) ensureForward(r *http.Request, namespace, name string) (uint16, error) {
	key := sessionKey(namespace, name)
	preferred := stickyLocalPort(key)

	a.mu.Lock()
	if existing, ok := a.active[key]; ok {
		// Refreshed on every hit so an app in active use is never reaped out
		// from under the user. Prefer the sticky port when the session already
		// holds it; otherwise keep whatever is live so we do not bounce the tab.
		existing.lastUsed = a.now()
		port := existing.localPort
		podName := existing.podName
		a.mu.Unlock()

		_, newest, resolveErr := a.resolveTarget(r, namespace, name)
		if resolveErr == nil && podName != "" && podName == newest && a.podReady(r.Context(), namespace, podName) {
			return port, nil
		}
		a.dropSession(key, existing.stopCh)
	} else {
		a.mu.Unlock()
	}

	remotePort, podName, err := a.resolveTarget(r, namespace, name)
	if err != nil {
		return 0, err
	}

	localPort, stopCh, err := a.startForward(r, key, namespace, podName, remotePort, preferred)
	if err != nil {
		// Sticky port may still be held for a moment after a pod restart.
		// Fall back to an ephemeral port rather than failing Open App.
		if preferred != 0 {
			localPort, stopCh, err = a.startForward(r, key, namespace, podName, remotePort, 0)
		}
		if err != nil {
			return 0, err
		}
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if existing, ok := a.active[key]; ok {
		// Another request won the race; use that session and stop ours.
		close(stopCh)
		existing.lastUsed = a.now()
		return existing.localPort, nil
	}

	if len(a.active) >= maxSessions {
		a.evictOldestLocked()
	}

	a.active[key] = &appForward{
		localPort: localPort,
		stopCh:    stopCh,
		podName:   podName,
		namespace: namespace,
		lastUsed:  a.now(),
	}
	if a.hosts == nil {
		a.hosts = make(map[string]string)
	}
	a.hosts[AppLocalHost(namespace, name)] = key

	// Keep the sticky localhost port alive across rollouts: when the attached
	// pod disappears, tear down and reattach to a Ready replacement without
	// forcing the user onto a new port.
	go a.maintainStickyForward(key, namespace, name, preferred, stopCh, podName)

	return localPort, nil
}

// maintainStickyForward watches the forwarded pod and rebuilds the session on
// the sticky port after rollout / rollback replaces it.
func (a *AppAccess) maintainStickyForward(
	key, namespace, name string,
	preferred uint16,
	stopCh chan struct{},
	podName string,
) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			currentReady := a.podReady(context.Background(), namespace, podName)
			req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
			_, newest, resolveErr := a.resolveTarget(req, namespace, name)
			if currentReady && (resolveErr != nil || newest == podName) {
				continue
			}
			// Pod gone, or a newer Ready replica exists after a rollout.
			a.dropSession(key, stopCh)

			// Rebuild only if nothing else claimed the key in the meantime.
			a.mu.Lock()
			_, stillActive := a.active[key]
			a.mu.Unlock()
			if stillActive {
				return
			}

			remotePort, newPod, err := a.resolveTarget(req, namespace, name)
			if err != nil {
				return
			}
			localPort, newStop, err := a.startForward(req, key, namespace, newPod, remotePort, preferred)
			if err != nil && preferred != 0 {
				localPort, newStop, err = a.startForward(req, key, namespace, newPod, remotePort, 0)
			}
			if err != nil {
				return
			}

			a.mu.Lock()
			if _, ok := a.active[key]; ok {
				close(newStop)
				a.mu.Unlock()
				return
			}
			if len(a.active) >= maxSessions {
				a.evictOldestLocked()
			}
			a.active[key] = &appForward{
				localPort: localPort,
				stopCh:    newStop,
				podName:   newPod,
				namespace: namespace,
				lastUsed:  a.now(),
			}
			if a.hosts == nil {
				a.hosts = make(map[string]string)
			}
			a.hosts[AppLocalHost(namespace, name)] = key
			a.mu.Unlock()

			// Continue watching the replacement pod.
			go a.maintainStickyForward(key, namespace, name, preferred, newStop, newPod)
			return
		}
	}
}

// dropSession stops a forward if it is still the active entry for key.
func (a *AppAccess) dropSession(key string, stopCh chan struct{}) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if cur, ok := a.active[key]; ok && cur.stopCh == stopCh {
		close(cur.stopCh)
		delete(a.active, key)
		a.unbindHostsForKeyLocked(key)
	}
}

// podReady reports whether the named pod still exists and is Ready. Used to
// invalidate Open App forwards after rollout / rollback replaces the pod set.
func (a *AppAccess) podReady(ctx context.Context, namespace, podName string) bool {
	if a == nil || !a.client.Available() || podName == "" {
		return false
	}
	pod, err := a.client.Clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return false
	}
	if pod.Status.Phase != corev1.PodRunning {
		return false
	}
	for _, c := range pod.Status.Conditions {
		if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	// No Ready condition yet — treat as not ready so we re-resolve.
	return false
}

func (a *AppAccess) startForward(
	r *http.Request,
	key, namespace, podName string,
	remotePort int32,
	localPort uint16,
) (uint16, chan struct{}, error) {
	if localPort != 0 && !localPortFree(localPort) {
		return 0, nil, fmt.Errorf("sticky port %d is busy", localPort)
	}

	stopCh := make(chan struct{}, 1)
	readyCh := make(chan struct{})
	errCh := make(chan error, 1)

	fw, err := a.newPortForward(namespace, podName, remotePort, localPort, stopCh, readyCh)
	if err != nil {
		return 0, nil, err
	}

	go func() {
		if err := fw.ForwardPorts(); err != nil {
			select {
			case errCh <- err:
			default:
			}
		}
		a.mu.Lock()
		if cur, ok := a.active[key]; ok && cur.stopCh == stopCh {
			delete(a.active, key)
		}
		a.mu.Unlock()
	}()

	select {
	case <-readyCh:
	case err := <-errCh:
		close(stopCh)
		return 0, nil, fmt.Errorf("port-forward failed: %w", err)
	case <-r.Context().Done():
		close(stopCh)
		return 0, nil, r.Context().Err()
	}

	ports, err := fw.GetPorts()
	if err != nil || len(ports) == 0 {
		close(stopCh)
		return 0, nil, fmt.Errorf("port-forward did not publish a local port")
	}
	return ports[0].Local, stopCh, nil
}

// stickyLocalPort maps a workload key onto a stable port in [18000, 18999].
// Redeploys that reopen the forward land on the same localhost address the
// user's browser tab already has, so they do not see a new random port.
func stickyLocalPort(key string) uint16 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return uint16(stickyPortBase + (h.Sum32() % stickyPortCount))
}

func localPortFree(port uint16) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

func (a *AppAccess) resolveTarget(r *http.Request, namespace, name string) (int32, string, error) {
	ctx := r.Context()

	svc, err := a.client.Clientset.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return 0, "", fmt.Errorf("service %q not found in namespace %q", name, namespace)
	}
	if len(svc.Spec.Ports) == 0 {
		return 0, "", fmt.Errorf("service %q has no ports", name)
	}

	remotePort := svc.Spec.Ports[0].TargetPort.IntVal
	if remotePort == 0 {
		remotePort = svc.Spec.Ports[0].Port
	}

	pods, err := a.client.Clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app=" + name,
	})
	if err != nil {
		return 0, "", fmt.Errorf("list pods: %w", err)
	}

	podName := newestReadyPodName(pods.Items)
	if podName == "" {
		return 0, "", fmt.Errorf("no running pod found for %s/%s", namespace, name)
	}

	return remotePort, podName, nil
}

func newestReadyPodName(pods []corev1.Pod) string {
	var best *corev1.Pod
	for i := range pods {
		p := &pods[i]
		if !isPodReadyForForward(p) {
			continue
		}
		if best == nil || p.CreationTimestamp.Time.After(best.CreationTimestamp.Time) {
			best = p
		}
	}
	if best == nil {
		return ""
	}
	return best.Name
}

func isPodReadyForForward(p *corev1.Pod) bool {
	if p == nil || p.DeletionTimestamp != nil {
		return false
	}
	if p.Status.Phase != corev1.PodRunning {
		return false
	}
	if len(p.Status.Conditions) == 0 {
		return true
	}
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func (a *AppAccess) newPortForward(
	namespace, podName string,
	remotePort int32,
	localPort uint16,
	stopCh, readyCh chan struct{},
) (*portforward.PortForwarder, error) {
	req := a.client.Clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(namespace).
		Name(podName).
		SubResource("portforward")

	transport, upgrader, err := spdy.RoundTripperFor(a.client.Config)
	if err != nil {
		return nil, fmt.Errorf("port-forward transport: %w", err)
	}

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, req.URL())
	spec := fmt.Sprintf("0:%d", remotePort)
	if localPort != 0 {
		spec = fmt.Sprintf("%d:%d", localPort, remotePort)
	}

	return portforward.NewOnAddresses(
		dialer,
		[]string{"127.0.0.1"},
		[]string{spec},
		stopCh,
		readyCh,
		io.Discard,
		io.Discard,
	)
}
