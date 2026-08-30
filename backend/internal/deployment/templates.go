package deployment

import idpv1 "github.com/idp/platform/backend/internal/gen/idp/v1"

// Deployment templates are the platform's golden paths: reviewed defaults for
// the stacks teams actually run, so a developer supplies an image, a name and a
// version instead of learning Kubernetes resource semantics.
//
// They are static server-side data rather than rows in a table. A template is a
// platform-team decision, not tenant data — storing them per project would let
// each team drift into its own defaults, which is the problem golden paths
// exist to solve. Changing one is a code review.
//
// Two rules the numbers below follow:
//
//   - Memory request equals memory limit. Memory is incompressible: a container
//     over its limit is OOM-killed, so a request below the limit only buys
//     scheduling onto a node that cannot honour it.
//   - CPU has a request but a deliberately looser limit. CPU is compressible —
//     throttling is survivable — so a tight limit costs latency during startup
//     spikes for no safety benefit.
var deploymentTemplates = []*idpv1.DeploymentTemplate{
	{
		Id:          "nodejs-api",
		Name:        "Node.js API",
		Description: "Express or Fastify HTTP service.",
		Category:    "Node.js",
		Replicas:    2,
		Port:        3000,
		Resources: &idpv1.ResourceLimits{
			CpuRequest: "100m", CpuLimit: "500m",
			MemoryRequest: "256Mi", MemoryLimit: "256Mi",
		},
		ReadinessProbe: &idpv1.Probe{
			Path: "/health", InitialDelaySeconds: 5, PeriodSeconds: 10, FailureThreshold: 3,
		},
		LivenessProbe: &idpv1.Probe{
			Path: "/health", InitialDelaySeconds: 20, PeriodSeconds: 15, FailureThreshold: 3,
		},
		ConfigVars: []*idpv1.KeyValue{
			{Key: "NODE_ENV", Value: "production"},
			{Key: "PORT", Value: "3000"},
			{Key: "LOG_LEVEL", Value: "info"},
		},
		SuggestedSecretKeys: []string{"DATABASE_URL", "JWT_SECRET"},
		ExampleImage:        "node:22-alpine",
		Rationale: "Node is single-threaded per process, so 500m CPU is ample; " +
			"replicas rather than cores provide throughput. 256Mi suits a typical " +
			"Express app with a modest dependency tree.",
		Autoscaling: &idpv1.Autoscaling{MinReplicas: 2, MaxReplicas: 6, CpuAverageUtilization: 70},
	},
	{
		Id:          "react-app",
		Name:        "React App",
		Description: "Static SPA build served by nginx.",
		Category:    "Frontend",
		Replicas:    2,
		Port:        80,
		Resources: &idpv1.ResourceLimits{
			CpuRequest: "50m", CpuLimit: "200m",
			MemoryRequest: "128Mi", MemoryLimit: "128Mi",
		},
		ReadinessProbe: &idpv1.Probe{
			Path: "/", InitialDelaySeconds: 3, PeriodSeconds: 10, FailureThreshold: 3,
		},
		// No liveness probe: nginx serving static files does not get into a
		// state a restart fixes, and a liveness probe would only add a way for
		// a healthy pod to be killed.
		ConfigVars:   []*idpv1.KeyValue{},
		ExampleImage: "nginx:1.27-alpine",
		Rationale: "Serving pre-built assets is nearly free. Runtime config must " +
			"be injected at build time or through a served config file — a SPA " +
			"cannot read container environment variables.",
		Autoscaling: &idpv1.Autoscaling{MinReplicas: 2, MaxReplicas: 8, CpuAverageUtilization: 70},
	},
	{
		Id:          "go-api",
		Name:        "Go API",
		Description: "Compiled Go HTTP service.",
		Category:    "Go",
		Replicas:    2,
		Port:        8080,
		Resources: &idpv1.ResourceLimits{
			CpuRequest: "100m", CpuLimit: "1000m",
			MemoryRequest: "128Mi", MemoryLimit: "128Mi",
		},
		ReadinessProbe: &idpv1.Probe{
			Path: "/readyz", InitialDelaySeconds: 2, PeriodSeconds: 10, FailureThreshold: 3,
		},
		LivenessProbe: &idpv1.Probe{
			Path: "/healthz", InitialDelaySeconds: 10, PeriodSeconds: 15, FailureThreshold: 3,
		},
		ConfigVars: []*idpv1.KeyValue{
			{Key: "PORT", Value: "8080"},
			{Key: "LOG_LEVEL", Value: "info"},
		},
		SuggestedSecretKeys: []string{"DATABASE_URL"},
		ExampleImage:        "golang:1.26-alpine",
		Rationale: "Go binaries start in milliseconds, so probe delays are short. " +
			"The runtime scales across cores, hence the higher CPU limit; 128Mi " +
			"is generous for a service without a large in-memory cache.",
		Autoscaling: &idpv1.Autoscaling{MinReplicas: 2, MaxReplicas: 8, CpuAverageUtilization: 70},
	},
	{
		Id:          "python-fastapi",
		Name:        "Python FastAPI",
		Description: "FastAPI service behind uvicorn.",
		Category:    "Python",
		Replicas:    2,
		Port:        8000,
		Resources: &idpv1.ResourceLimits{
			CpuRequest: "200m", CpuLimit: "1000m",
			MemoryRequest: "512Mi", MemoryLimit: "512Mi",
		},
		ReadinessProbe: &idpv1.Probe{
			Path: "/health", InitialDelaySeconds: 10, PeriodSeconds: 10, FailureThreshold: 3,
		},
		LivenessProbe: &idpv1.Probe{
			Path: "/health", InitialDelaySeconds: 30, PeriodSeconds: 15, FailureThreshold: 3,
		},
		ConfigVars: []*idpv1.KeyValue{
			{Key: "PORT", Value: "8000"},
			{Key: "LOG_LEVEL", Value: "info"},
			{Key: "PYTHONUNBUFFERED", Value: "1"},
		},
		SuggestedSecretKeys: []string{"DATABASE_URL", "SECRET_KEY"},
		ExampleImage:        "python:3.13-slim",
		Rationale: "PYTHONUNBUFFERED is set because CPython buffers stdout when it " +
			"is a pipe, which makes container logs appear only in bursts. Import " +
			"time is slow, so the readiness delay is longer than Go's.",
		Autoscaling: &idpv1.Autoscaling{MinReplicas: 2, MaxReplicas: 6, CpuAverageUtilization: 70},
	},
	{
		Id:          "spring-boot",
		Name:        "Spring Boot",
		Description: "JVM service with Actuator endpoints.",
		Category:    "Java",
		Replicas:    2,
		Port:        8080,
		Resources: &idpv1.ResourceLimits{
			CpuRequest: "500m", CpuLimit: "2000m",
			MemoryRequest: "1Gi", MemoryLimit: "1Gi",
		},
		ReadinessProbe: &idpv1.Probe{
			Path: "/actuator/health/readiness", InitialDelaySeconds: 20,
			PeriodSeconds: 10, FailureThreshold: 6,
		},
		LivenessProbe: &idpv1.Probe{
			Path: "/actuator/health/liveness", InitialDelaySeconds: 60,
			PeriodSeconds: 15, FailureThreshold: 3,
		},
		ConfigVars: []*idpv1.KeyValue{
			{Key: "SERVER_PORT", Value: "8080"},
			{Key: "SPRING_PROFILES_ACTIVE", Value: "production"},
			// Without this the JVM sizes its heap from the node's memory, not
			// the container limit, and gets OOM-killed on a large node.
			{Key: "JAVA_TOOL_OPTIONS", Value: "-XX:MaxRAMPercentage=75.0"},
		},
		SuggestedSecretKeys: []string{"SPRING_DATASOURCE_PASSWORD"},
		ExampleImage:        "eclipse-temurin:21-jre-alpine",
		Rationale: "JVM startup dominates: the liveness delay is 60s so a slow " +
			"boot is not mistaken for a hang, and readiness tolerates 6 failures. " +
			"Actuator's split readiness/liveness endpoints are used rather than " +
			"pointing both at /actuator/health.",
	},
	{
		Id:          "mongodb",
		Name:        "MongoDB",
		Description: "MongoDB with a PersistentVolumeClaim (replicas forced to 1).",
		Category:    "Database",
		Replicas:    1,
		Port:        27017,
		Resources: &idpv1.ResourceLimits{
			CpuRequest: "100m", CpuLimit: "500m",
			MemoryRequest: "512Mi", MemoryLimit: "512Mi",
		},
		ConfigVars: []*idpv1.KeyValue{
			{Key: "MONGO_INITDB_DATABASE", Value: "app"},
		},
		SuggestedSecretKeys: []string{"MONGO_INITDB_ROOT_USERNAME", "MONGO_INITDB_ROOT_PASSWORD"},
		ExampleImage:        "mongo:7",
		Rationale: "A single replica with a PVC keeps data across restarts. " +
			"HTTP probes are omitted — Mongo speaks its own wire protocol. " +
			"Ingress is disabled; apps reach it via the in-cluster Service DNS.",
	},
	{
		Id:          "postgres",
		Name:        "PostgreSQL",
		Description: "PostgreSQL with a PersistentVolumeClaim (replicas forced to 1).",
		Category:    "Database",
		Replicas:    1,
		Port:        5432,
		Resources: &idpv1.ResourceLimits{
			CpuRequest: "100m", CpuLimit: "500m",
			MemoryRequest: "512Mi", MemoryLimit: "512Mi",
		},
		ConfigVars: []*idpv1.KeyValue{
			{Key: "POSTGRES_DB", Value: "app"},
			{Key: "PGDATA", Value: "/var/lib/postgresql/data/pgdata"},
		},
		SuggestedSecretKeys: []string{"POSTGRES_USER", "POSTGRES_PASSWORD"},
		ExampleImage:        "postgres:16-alpine",
		Rationale: "RWO volumes require a single replica. PGDATA points at a " +
			"subdirectory so the official image can initialise an empty PVC.",
	},
	{
		Id:          "mysql",
		Name:        "MySQL",
		Description: "MySQL with a PersistentVolumeClaim (replicas forced to 1).",
		Category:    "Database",
		Replicas:    1,
		Port:        3306,
		Resources: &idpv1.ResourceLimits{
			CpuRequest: "100m", CpuLimit: "500m",
			MemoryRequest: "512Mi", MemoryLimit: "512Mi",
		},
		ConfigVars: []*idpv1.KeyValue{
			{Key: "MYSQL_DATABASE", Value: "app"},
		},
		SuggestedSecretKeys: []string{"MYSQL_ROOT_PASSWORD", "MYSQL_USER", "MYSQL_PASSWORD"},
		ExampleImage:        "mysql:8.4",
		Rationale: "Single replica plus PVC. Set MYSQL_ROOT_PASSWORD (required by " +
			"the official image) before the first start.",
	},
}

// Templates returns the catalogue. Each call rebuilds the messages so a caller
// mutating one cannot corrupt the shared catalogue for every later request —
// these would otherwise be pointers into package-level state.
func Templates() []*idpv1.DeploymentTemplate {
	out := make([]*idpv1.DeploymentTemplate, 0, len(deploymentTemplates))
	for _, t := range deploymentTemplates {
		out = append(out, cloneTemplate(t))
	}
	return out
}

func cloneTemplate(t *idpv1.DeploymentTemplate) *idpv1.DeploymentTemplate {
	clone := &idpv1.DeploymentTemplate{
		Id:                  t.Id,
		Name:                t.Name,
		Description:         t.Description,
		Category:            t.Category,
		Replicas:            t.Replicas,
		Port:                t.Port,
		ExampleImage:        t.ExampleImage,
		Rationale:           t.Rationale,
		SuggestedSecretKeys: append([]string(nil), t.SuggestedSecretKeys...),
		ConfigVars:          make([]*idpv1.KeyValue, 0, len(t.ConfigVars)),
	}
	if t.Resources != nil {
		clone.Resources = &idpv1.ResourceLimits{
			CpuRequest:    t.Resources.CpuRequest,
			CpuLimit:      t.Resources.CpuLimit,
			MemoryRequest: t.Resources.MemoryRequest,
			MemoryLimit:   t.Resources.MemoryLimit,
		}
	}
	clone.ReadinessProbe = cloneProbe(t.ReadinessProbe)
	clone.LivenessProbe = cloneProbe(t.LivenessProbe)
	for _, kv := range t.ConfigVars {
		clone.ConfigVars = append(clone.ConfigVars, &idpv1.KeyValue{Key: kv.Key, Value: kv.Value})
	}
	return clone
}

func cloneProbe(p *idpv1.Probe) *idpv1.Probe {
	if p == nil {
		return nil
	}
	return &idpv1.Probe{
		Path:                p.Path,
		Port:                p.Port,
		InitialDelaySeconds: p.InitialDelaySeconds,
		TimeoutSeconds:      p.TimeoutSeconds,
		PeriodSeconds:       p.PeriodSeconds,
		FailureThreshold:    p.FailureThreshold,
	}
}
