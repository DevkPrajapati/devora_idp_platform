package server

import (
	"context"
	"fmt"
	"net/http"

	"connectrpc.com/connect"
	"github.com/idp/platform/backend/internal/audit"
	"github.com/idp/platform/backend/internal/auth"
	"github.com/idp/platform/backend/internal/build"
	"github.com/idp/platform/backend/internal/cluster"
	"github.com/idp/platform/backend/internal/config"
	"github.com/idp/platform/backend/internal/database"
	"github.com/idp/platform/backend/internal/dbbrowse"
	"github.com/idp/platform/backend/internal/deployment"
	idpv1connect "github.com/idp/platform/backend/internal/gen/idp/v1/idpv1connect"
	"github.com/idp/platform/backend/internal/health"
	"github.com/idp/platform/backend/internal/keycloak"
	"github.com/idp/platform/backend/internal/kubernetes"
	"github.com/idp/platform/backend/internal/metrics"
	"github.com/idp/platform/backend/internal/middleware"
	"github.com/idp/platform/backend/internal/namespace"
	"github.com/idp/platform/backend/internal/pkg/secretbox"
	"github.com/idp/platform/backend/internal/project"
	"github.com/idp/platform/backend/internal/registry"
	"github.com/idp/platform/backend/internal/repository"
	"github.com/idp/platform/backend/internal/storage"
	"go.uber.org/zap"
)

// Server wraps the HTTP server and its dependencies.
type Server struct {
	cfg    *config.Config
	logger *zap.Logger
	pool   *database.Pool
	http   *http.Server
	// stopReconciler ends the background build loop on shutdown. Nil when
	// builds are disabled or no cluster is connected.
	stopReconciler context.CancelFunc
	// stopSampler ends the metrics sampling loop. Nil when no cluster is
	// connected, since there would be nothing to sample.
	stopSampler context.CancelFunc
	// appAccess holds live port-forwards that must be torn down on shutdown.
	// Nil when no cluster is connected.
	appAccess *kubernetes.AppAccess
}

// New creates a configured HTTP server with all routes and middleware.
func New(cfg *config.Config, logger *zap.Logger, pool *database.Pool) *Server {
	mux := http.NewServeMux()

	k8sClient, err := kubernetes.NewClient(cfg.Kubernetes)
	if err != nil {
		logger.Warn("kubernetes client unavailable", zap.Error(err))
	}

	validator := auth.NewValidator(auth.ValidatorConfig{
		Issuer:   cfg.Auth.Issuer,
		Audience: cfg.Auth.Audience,
		JWKSURL:  cfg.Auth.JWKSURL,
		Enabled:  cfg.Auth.Enabled,
	})

	// Order matters: authentication puts the user on the context, then
	// authorization reads it. Reversing them would evaluate an empty context.
	interceptors := connect.WithInterceptors(
		auth.NewInterceptor(validator),
		auth.NewAuthorizationInterceptor(),
	)

	// Signs the short-lived tickets that authorize /apps/ redirects. Derived
	// from the encryption key so every replica agrees; falls back to a random
	// per-process key, which is correct for a single replica.
	ticketSigner, ticketErr := auth.NewTicketSigner(cfg.Security.EncryptionKey)
	if ticketErr != nil {
		logger.Error("app access ticket signer unavailable", zap.Error(ticketErr))
	} else if cfg.Security.EncryptionKey == "" {
		logger.Warn("IDP_ENCRYPTION_KEY not set; app access tickets use a per-process key " +
			"and will not verify across replicas or restarts")
	}

	// A missing or malformed key is fatal for credential storage but not for
	// the rest of the platform, so it degrades to a warning here and to an
	// explicit FailedPrecondition on any RPC that would store a secret.
	encryptionBox, keyErr := secretbox.NewFromEncodedKey(cfg.Security.EncryptionKey)
	if keyErr != nil {
		logger.Error("invalid IDP_ENCRYPTION_KEY; registry credentials disabled", zap.Error(keyErr))
		encryptionBox = nil
	} else if encryptionBox == nil {
		logger.Warn("IDP_ENCRYPTION_KEY not set; registry credential storage is disabled")
	}

	auditRepo := repository.NewAuditRepository(pool)
	namespaceRepo := repository.NewNamespaceRepository(pool)
	projectRepo := repository.NewProjectRepository(pool)
	registryRepo := repository.NewRegistryRepository(pool)
	auditService := audit.NewService(auditRepo)
	registryService := registry.NewService(
		registryRepo, projectRepo, namespaceRepo, k8sClient,
		encryptionBox, registry.NewHTTPProber(), auditService,
	)
	namespaceService := namespace.NewService(namespaceRepo, projectRepo, k8sClient, registryService, auditService)
	deploymentService := deployment.NewService(k8sClient, namespaceRepo, registryService, auditService)
	clusterService := cluster.NewService(k8sClient)
	storageService := storage.NewService(k8sClient)
	dbbrowseService := dbbrowse.NewService(k8sClient)

	kcBase, kcRealm := keycloak.ParseIssuer(cfg.Auth.Issuer)
	kcAdmin := keycloak.NewAdmin(keycloak.AdminConfig{
		BaseURL:      kcBase,
		Realm:        kcRealm,
		ClientID:     cfg.Auth.AdminClientID,
		ClientSecret: cfg.Auth.AdminClientSecret,
	})
	if cfg.Auth.Enabled && kcAdmin.Enabled() {
		logger.Info("keycloak admin provisioning enabled",
			zap.String("base_url", kcBase),
			zap.String("realm", kcRealm),
			zap.String("client_id", cfg.Auth.AdminClientID),
		)
	} else if cfg.Auth.Enabled {
		logger.Warn("keycloak admin provisioning disabled; Add Member will not create login accounts")
	}
	projectService := project.NewService(projectRepo, auditService, kcAdmin)

	buildRepo := repository.NewBuildRepository(pool)
	buildService := build.NewService(buildRepo, projectRepo, k8sClient, encryptionBox, auditService, logger,
		build.Config{
			Namespace:   cfg.Build.Namespace,
			KanikoImage: cfg.Build.KanikoImage,
			PublicURL:   cfg.Build.PublicURL,
			Resources:   build.BuildResourceDefaults(),
		})

	healthCheckers := []health.Checker{health.NewDatabaseChecker(pool)}
	if k8sClient != nil {
		healthCheckers = append(healthCheckers, health.NewKubernetesChecker(k8sClient))
	}
	healthService := health.NewService(cfg.App.Version, healthCheckers...)
	healthHandler := health.NewHandler(healthService)

	auditHandler := audit.NewHandler(auditService)
	namespaceHandler := namespace.NewHandler(namespaceService)
	deploymentHandler := deployment.NewHandler(deploymentService)
	clusterHandler := cluster.NewHandler(clusterService)
	storageHandler := storage.NewHandler(storageService)
	dbbrowseHandler := dbbrowse.NewHandler(dbbrowseService)
	projectHandler := project.NewHandler(projectService)
	registryHandler := registry.NewHandler(registryService)
	buildHandler := build.NewHandler(buildService)

	{
		path, handler := idpv1connect.NewHealthServiceHandler(healthHandler, interceptors)
		mux.Handle(path, handler)
	}
	{
		path, handler := idpv1connect.NewAuditServiceHandler(auditHandler, interceptors)
		mux.Handle(path, handler)
	}
	{
		path, handler := idpv1connect.NewNamespaceServiceHandler(namespaceHandler, interceptors)
		mux.Handle(path, handler)
	}
	{
		path, handler := idpv1connect.NewDeploymentServiceHandler(deploymentHandler, interceptors)
		mux.Handle(path, handler)
	}
	{
		path, handler := idpv1connect.NewClusterServiceHandler(clusterHandler, interceptors)
		mux.Handle(path, handler)
	}
	{
		path, handler := idpv1connect.NewStorageServiceHandler(storageHandler, interceptors)
		mux.Handle(path, handler)
	}
	{
		path, handler := idpv1connect.NewDatabaseServiceHandler(dbbrowseHandler, interceptors)
		mux.Handle(path, handler)
	}
	{
		path, handler := idpv1connect.NewProjectServiceHandler(projectHandler, interceptors)
		mux.Handle(path, handler)
	}
	{
		path, handler := idpv1connect.NewRegistryServiceHandler(registryHandler, interceptors)
		mux.Handle(path, handler)
	}
	{
		path, handler := idpv1connect.NewBuildServiceHandler(buildHandler, interceptors)
		mux.Handle(path, handler)
	}

	// GET probes for orchestrators. Outside the Connect surface because every
	// Connect procedure is a POST, which httpGet probes cannot issue.
	mux.Handle(health.LivenessPath, healthHandler.Liveness())
	mux.Handle(health.ReadinessPath, healthHandler.Readiness())

	// Deliberately outside the auth interceptor: the caller is a git provider,
	// not a platform user. Authentication is by HMAC over the payload, which
	// the handler enforces before parsing anything.
	mux.Handle(build.WebhookPath+"/", build.WebhookHandler(buildService, logger))

	// Mints the signed tickets that /apps/ redemptions require. Authenticates
	// the caller itself, since it sits outside the Connect interceptor chain.
	mux.Handle(AppTicketPath, appTicketHandler(validator, ticketSigner, logger))

	// Platform configuration and the resource-usage trend the dashboard draws.
	metricsHistory := metrics.New()
	mux.Handle(PlatformPath, platformHandler(cfg, validator, k8sClient, metricsHistory))

	// Click-to-open: /apps/{namespace}/{name} port-forwards to the workload and
	// redirects the browser to 127.0.0.1 — no kubectl or /etc/hosts required.
	//
	// The ticket signer is the authorization gate. Passing it in is what keeps
	// this endpoint from being an unauthenticated tunnel into any pod in the
	// cluster; NewAppAccess returns nil rather than an open handler if it is
	// missing.
	appAccess := kubernetes.NewAppAccess(k8sClient, ticketSigner)
	if appAccess != nil {
		mux.Handle(kubernetes.AppsPathPrefix, appAccess.Handler())
	}

	// Prometheus scrape target. Deliberately unauthenticated: a scraper is not
	// a platform user, and the series expose no tenant data — only method,
	// route class, status, and latency. Restrict it at the network layer if
	// the deployment needs that; the Ingress does not route /metrics.
	mux.Handle(middleware.MetricsPath, middleware.MetricsHandler())

	// Outermost first. RequestID leads so the ID exists before anything logs;
	// Metrics wraps Logging so recorded latency includes log serialisation;
	// Recovery sits innermost so a panic is still counted and logged rather
	// than escaping as a bare connection reset.
	rootHandler := middleware.RequestID(
		middleware.CORS(cfg.CORS.AllowedOrigins)(
			middleware.Metrics(
				middleware.Logging(logger)(
					middleware.Recovery(logger)(mux),
				),
			),
		),
	)

	// Connect RPC clients negotiate HTTP/2 without TLS, which used to require
	// wrapping the handler in x/net/http2/h2c. That package is deprecated as of
	// the x/net release this module now pins; Protocols is the stdlib
	// replacement and covers the same three cases — HTTP/1.1, h2c with a prior
	// -knowledge preface, and h2c via Upgrade.
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)

	httpServer := &http.Server{
		Addr:    cfg.Server.Address,
		Handler: rootHandler,
		Protocols: protocols,
		// ReadHeaderTimeout covers slowloris; ReadTimeout is left unset so
		// Connect server streams (e.g. StreamPodLogs) are not killed mid-tail.
		// WriteTimeout must stay 0 for the same reason — a non-zero value is a
		// hard cap on the entire response lifetime in net/http.
		ReadHeaderTimeout: cfg.Server.ReadTimeout,
		WriteTimeout:      0,
	}

	server := &Server{
		cfg:       cfg,
		logger:    logger,
		pool:      pool,
		http:      httpServer,
		appAccess: appAccess,
	}

	// Samples cluster utilisation into the ring buffer the dashboard reads.
	// Only started with a cluster attached — otherwise every tick would fail
	// and the history would stay empty anyway.
	if k8sClient != nil {
		samplerCtx, cancelSampler := context.WithCancel(context.Background())
		server.stopSampler = cancelSampler
		go metricsHistory.Run(samplerCtx, clusterService, metrics.DefaultInterval)
	}

	// The reconciler advances build Jobs to a terminal state and runs the
	// deploy step. It is a background loop rather than request-driven because
	// a build outlives the request that started it.
	if cfg.Build.Enabled && k8sClient != nil {
		reconcilerCtx, cancel := context.WithCancel(context.Background())
		server.stopReconciler = cancel
		go buildService.StartReconciler(reconcilerCtx, cfg.Build.PollInterval)
		logger.Info("build reconciler started",
			zap.String("namespace", cfg.Build.Namespace),
			zap.Duration("interval", cfg.Build.PollInterval))
	}

	return server
}

// Start begins serving HTTP requests.
func (s *Server) Start() error {
	s.logger.Info("starting server",
		zap.String("address", s.cfg.Server.Address),
		zap.String("version", s.cfg.App.Version),
		zap.String("env", s.cfg.App.Env),
	)
	if err := s.http.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("listen and serve: %w", err)
	}
	return nil
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("shutting down server")
	// Stopped before the HTTP server so the loop is not still writing to the
	// database while the pool is being closed.
	if s.stopReconciler != nil {
		s.stopReconciler()
	}
	if s.stopSampler != nil {
		s.stopSampler()
	}
	// Closes every live port-forward and its reaper goroutine. Without this the
	// SPDY streams stay open until the process exits.
	s.appAccess.Close()
	if err := s.http.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown server: %w", err)
	}
	s.pool.Close()
	return nil
}
