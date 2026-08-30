package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config holds all application configuration loaded from environment variables.
type Config struct {
	Server     ServerConfig
	Database   DatabaseConfig
	Auth       AuthConfig
	Kubernetes KubernetesConfig
	Security   SecurityConfig
	Build      BuildConfig
	CORS       CORSConfig
	Log        LogConfig
	App        AppConfig
}

// BuildConfig holds git build-and-deploy settings.
type BuildConfig struct {
	// Enabled turns the reconciler on. Off, repositories can still be
	// registered but nothing is executed.
	Enabled bool
	// Namespace build Jobs run in, kept apart from application namespaces so a
	// build pod cannot read application Secrets.
	Namespace string
	// KanikoImage overrides the builder image.
	KanikoImage string
	// PublicURL is the externally reachable base URL, used to render the
	// webhook endpoint users paste into their git provider.
	PublicURL string
	// PollInterval is how often build Jobs are checked for completion.
	PollInterval time.Duration
}

// SecurityConfig holds platform-wide cryptographic settings.
type SecurityConfig struct {
	// EncryptionKey is a base64- or hex-encoded 32-byte key used to seal
	// secrets at rest (registry passwords today, secret env values next).
	// Empty disables features that would otherwise store secrets in plaintext,
	// rather than degrading to plaintext silently.
	EncryptionKey string
}

type ServerConfig struct {
	Host            string
	Port            int
	Address         string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	ShutdownTimeout time.Duration
}

type DatabaseConfig struct {
	URL      string
	MaxConns int32
	MinConns int32
}

type AuthConfig struct {
	Issuer   string
	Audience string
	JWKSURL  string
	Enabled  bool
	// AdminClientID / AdminClientSecret authenticate the backend to the
	// Keycloak Admin API (client_credentials on the confidential idp-backend
	// client). Empty secret disables user provisioning from Add Member.
	AdminClientID     string
	AdminClientSecret string
}

// CORSConfig holds browser origin policy.
type CORSConfig struct {
	// AllowedOrigins is an exact-match allowlist. Exact matching rather than
	// wildcards is deliberate: responses carry
	// Access-Control-Allow-Credentials, and a reflected or wildcard origin
	// combined with credentials lets any site read authenticated responses.
	AllowedOrigins []string
}

type KubernetesConfig struct {
	Kubeconfig string
	InCluster  bool
	// RequestTimeout caps each Kubernetes API call (default 15s).
	RequestTimeout time.Duration
	// DialTimeout caps TCP connect time (default 5s).
	DialTimeout time.Duration
	// IngressEnabled controls automatic Ingress creation. Turn it off on
	// clusters with no ingress controller, where every generated Ingress would
	// sit unrouted while the UI advertised a URL that never resolves.
	IngressEnabled bool
	// IngressDomain is the suffix for generated hostnames, e.g. idp.local.
	IngressDomain string
	// IngressClass is the IngressClass name, e.g. nginx.
	IngressClass string
	// IngressTLSSecret, when set, is attached to every generated Ingress and
	// switches published URLs to https.
	IngressTLSSecret string
}

type LogConfig struct {
	Level  string
	Format string
}

type AppConfig struct {
	Version string
	Env     string
}

// Load reads configuration from environment variables and optional .env file.
func Load() (*Config, error) {
	// .env has to reach the process environment before viper looks at it.
	//
	// viper's "env" config type parses the file into a flat map keyed by the
	// literal variable name (idp_encryption_key), but every lookup below uses a
	// dotted key (security.encryption_key) resolved through BindEnv and
	// AutomaticEnv — and both consult the OS environment only, never the parsed
	// map. Without this bridge the file is read successfully and then ignored,
	// so every setting silently falls back to its default. That is how a
	// configured IDP_ENCRYPTION_KEY could still arrive empty.
	loadDotEnv(".env")

	v := viper.New()
	v.SetConfigFile(".env")
	v.SetConfigType("env")
	_ = v.ReadInConfig()

	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	setDefaults(v)

	cfg := &Config{
		Server: ServerConfig{
			Host:            v.GetString("server.host"),
			Port:            v.GetInt("server.port"),
			ReadTimeout:     v.GetDuration("server.read_timeout"),
			WriteTimeout:    v.GetDuration("server.write_timeout"),
			ShutdownTimeout: v.GetDuration("server.shutdown_timeout"),
		},
		Database: DatabaseConfig{
			URL:      v.GetString("database.url"),
			MaxConns: int32(v.GetInt("database.max_conns")),
			MinConns: int32(v.GetInt("database.min_conns")),
		},
		Auth: AuthConfig{
			Issuer:            v.GetString("auth.issuer"),
			Audience:          v.GetString("auth.audience"),
			JWKSURL:           v.GetString("auth.jwks_url"),
			Enabled:           v.GetBool("auth.enabled"),
			AdminClientID:     v.GetString("auth.admin_client_id"),
			AdminClientSecret: v.GetString("auth.admin_client_secret"),
		},
		Kubernetes: KubernetesConfig{
			Kubeconfig:       v.GetString("kubernetes.kubeconfig"),
			InCluster:        v.GetBool("kubernetes.in_cluster"),
			RequestTimeout:   v.GetDuration("kubernetes.request_timeout"),
			DialTimeout:      v.GetDuration("kubernetes.dial_timeout"),
			IngressEnabled:   v.GetBool("ingress.enabled"),
			IngressDomain:    v.GetString("ingress.domain"),
			IngressClass:     v.GetString("ingress.class"),
			IngressTLSSecret: v.GetString("ingress.tls_secret"),
		},
		Security: SecurityConfig{
			EncryptionKey: v.GetString("security.encryption_key"),
		},
		CORS: CORSConfig{
			AllowedOrigins: splitAndTrim(v.GetString("cors.allowed_origins")),
		},
		Build: BuildConfig{
			Enabled:      v.GetBool("build.enabled"),
			Namespace:    v.GetString("build.namespace"),
			KanikoImage:  v.GetString("build.kaniko_image"),
			PublicURL:    v.GetString("build.public_url"),
			PollInterval: v.GetDuration("build.poll_interval"),
		},
		Log: LogConfig{
			Level:  v.GetString("log.level"),
			Format: v.GetString("log.format"),
		},
		App: AppConfig{
			Version: v.GetString("app.version"),
			Env:     v.GetString("app.env"),
		},
	}

	cfg.Server.Address = fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)

	if err := validate(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// loadDotEnv copies KEY=VALUE lines from path into the process environment.
//
// An absent file is normal — in a container the values come from the
// environment directly — so a missing .env is not an error.
//
// A variable already present in the environment is left alone: an explicit
// export, or a value injected by Kubernetes, must outrank a checked-in file.
func loadDotEnv(path string) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(strings.TrimPrefix(key, "export "))
		if key == "" {
			continue
		}

		// Quotes are part of dotenv syntax, not of the value; leaving them on
		// would turn a base64 key into an unparseable one.
		value = strings.TrimSpace(value)
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}

		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		_ = os.Setenv(key, value)
	}
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("server.host", "0.0.0.0")
	v.SetDefault("server.port", 8090)
	v.SetDefault("server.read_timeout", 15*time.Second)
	v.SetDefault("server.write_timeout", 15*time.Second)
	v.SetDefault("server.shutdown_timeout", 30*time.Second)

	// 5434 matches deploy/docker-compose.yml and the Makefile. It previously
	// read 5433 here, so a backend started without a .env silently connected to
	// the wrong port.
	v.SetDefault("database.url", "postgres://idp:idp_dev_password@localhost:5434/idp?sslmode=disable&connect_timeout=5")
	v.SetDefault("database.max_conns", 25)
	v.SetDefault("database.min_conns", 5)

	v.SetDefault("auth.issuer", "http://localhost:8080/realms/idp")
	v.SetDefault("auth.audience", "idp-backend")
	v.SetDefault("auth.jwks_url", "http://localhost:8080/realms/idp/protocol/openid-connect/certs")
	// Secure by default. This previously defaulted to false, which injected an
	// admin dev-user into every request; an operator who never set the variable
	// got an open API and no signal that anything was wrong. Turning it off is
	// now an explicit, development-only opt-out enforced in validate().
	v.SetDefault("auth.enabled", true)
	v.SetDefault("auth.admin_client_id", "idp-backend")
	// Matches deploy/keycloak/realm-export.json. Override in production.
	v.SetDefault("auth.admin_client_secret", "idp-backend-dev-secret")

	v.SetDefault("kubernetes.in_cluster", false)
	// A cluster-wide list (every pod, every namespace) on a local single-node
	// cluster regularly needs more than five seconds, and more still while the
	// kubelet is under load. The five-second default cut those calls off and
	// surfaced them as "cluster unavailable", even though the cluster was only
	// slow. The client's own fallback already used 15s; this default was the
	// reason that fallback never applied.
	v.SetDefault("kubernetes.request_timeout", 15*time.Second)
	v.SetDefault("kubernetes.dial_timeout", 5*time.Second)

	v.SetDefault("build.enabled", true)
	v.SetDefault("build.namespace", "idp-builds")
	v.SetDefault("build.poll_interval", 10*time.Second)

	v.SetDefault("ingress.enabled", true)
	v.SetDefault("ingress.domain", "idp.local")
	v.SetDefault("ingress.class", "nginx")

	v.SetDefault("cors.allowed_origins", "http://localhost:5173")

	v.SetDefault("log.level", "info")
	v.SetDefault("log.format", "json")

	v.SetDefault("app.version", "0.1.0")
	v.SetDefault("app.env", "development")

	_ = v.BindEnv("server.host", "SERVER_HOST")
	_ = v.BindEnv("server.port", "SERVER_PORT")
	_ = v.BindEnv("server.read_timeout", "SERVER_READ_TIMEOUT")
	_ = v.BindEnv("server.write_timeout", "SERVER_WRITE_TIMEOUT")
	_ = v.BindEnv("server.shutdown_timeout", "SERVER_SHUTDOWN_TIMEOUT")

	_ = v.BindEnv("database.url", "DATABASE_URL")
	_ = v.BindEnv("database.max_conns", "DATABASE_MAX_CONNS")
	_ = v.BindEnv("database.min_conns", "DATABASE_MIN_CONNS")

	_ = v.BindEnv("auth.issuer", "AUTH_ISSUER")
	_ = v.BindEnv("auth.audience", "AUTH_AUDIENCE")
	_ = v.BindEnv("auth.jwks_url", "AUTH_JWKS_URL")
	_ = v.BindEnv("auth.enabled", "AUTH_ENABLED")
	_ = v.BindEnv("auth.admin_client_id", "AUTH_ADMIN_CLIENT_ID")
	_ = v.BindEnv("auth.admin_client_secret", "AUTH_ADMIN_CLIENT_SECRET")

	_ = v.BindEnv("security.encryption_key", "IDP_ENCRYPTION_KEY")

	_ = v.BindEnv("kubernetes.kubeconfig", "KUBECONFIG")
	_ = v.BindEnv("kubernetes.in_cluster", "KUBERNETES_IN_CLUSTER")
	_ = v.BindEnv("kubernetes.request_timeout", "KUBERNETES_REQUEST_TIMEOUT")
	_ = v.BindEnv("kubernetes.dial_timeout", "KUBERNETES_DIAL_TIMEOUT")

	_ = v.BindEnv("build.enabled", "IDP_BUILD_ENABLED")
	_ = v.BindEnv("build.namespace", "IDP_BUILD_NAMESPACE")
	_ = v.BindEnv("build.kaniko_image", "IDP_BUILD_KANIKO_IMAGE")
	_ = v.BindEnv("build.public_url", "IDP_PUBLIC_URL")
	_ = v.BindEnv("build.poll_interval", "IDP_BUILD_POLL_INTERVAL")

	_ = v.BindEnv("ingress.enabled", "IDP_INGRESS_ENABLED")
	_ = v.BindEnv("ingress.domain", "IDP_INGRESS_DOMAIN")
	_ = v.BindEnv("ingress.class", "IDP_INGRESS_CLASS")
	_ = v.BindEnv("ingress.tls_secret", "IDP_INGRESS_TLS_SECRET")

	_ = v.BindEnv("cors.allowed_origins", "IDP_CORS_ALLOWED_ORIGINS")

	_ = v.BindEnv("log.level", "LOG_LEVEL")
	_ = v.BindEnv("log.format", "LOG_FORMAT")

	_ = v.BindEnv("app.version", "APP_VERSION")
	_ = v.BindEnv("app.env", "APP_ENV")
}

// splitAndTrim parses a comma-separated env var into a clean slice, dropping
// empty entries so a trailing comma cannot introduce an empty origin — which
// would otherwise match a request that sent no Origin header at all.
func splitAndTrim(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// IsDevelopment reports whether the process is running in the one environment
// where relaxed defaults are acceptable.
func (c *Config) IsDevelopment() bool {
	return strings.EqualFold(strings.TrimSpace(c.App.Env), "development")
}

func validate(cfg *Config) error {
	if cfg.Database.URL == "" {
		return fmt.Errorf("database.url is required")
	}
	if cfg.Server.Port <= 0 || cfg.Server.Port > 65535 {
		return fmt.Errorf("server.port must be between 1 and 65535")
	}

	// Refusing to boot is the point. A misconfigured production deployment that
	// starts anyway serves every request as an administrator, and nothing in
	// the running system looks wrong until someone notices the audit log.
	if !cfg.Auth.Enabled && !cfg.IsDevelopment() {
		return fmt.Errorf(
			"auth.enabled is false but app.env is %q: authentication may only be "+
				"disabled when APP_ENV=development", cfg.App.Env)
	}

	if len(cfg.CORS.AllowedOrigins) == 0 {
		return fmt.Errorf("cors.allowed_origins must list at least one origin")
	}
	for _, origin := range cfg.CORS.AllowedOrigins {
		// A wildcard cannot be combined with credentialed requests, and the
		// platform always sends credentials. Rejecting it here beats letting a
		// browser reject every response at runtime.
		if origin == "*" {
			return fmt.Errorf("cors.allowed_origins may not contain \"*\" because " +
				"credentialed requests require explicit origins")
		}
	}

	return nil
}
