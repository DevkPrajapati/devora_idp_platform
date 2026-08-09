# Tool paths are derived from `go env GOPATH` rather than a `command -v` probe.
# The probe was POSIX-only: on Windows make runs recipes through cmd.exe, where
# `command -v` does not exist and `2> /dev/null` fails outright because /dev is
# not a real directory — which left these variables holding uninterpreted text.
#
# `go env GOPATH` runs identically under cmd.exe and sh, and the trailing
# executable name resolves through PATHEXT on Windows and directly elsewhere.
# Override any of these if the tools live somewhere else:
#   make GOOSE=goose migrate-up
GOPATH_BIN := $(shell go env GOPATH)/bin
GOOSE ?= $(GOPATH_BIN)/goose
SQLC ?= $(GOPATH_BIN)/sqlc
BUF ?= $(GOPATH_BIN)/buf

# `?=` defers to an exported DATABASE_URL. It replaces a `$${VAR:-default}`
# shell expansion, which cmd.exe passes through as a literal string.
DATABASE_URL ?= postgres://idp:idp_dev_password@localhost:5434/idp?sslmode=disable

.PHONY: help dev-up dev-down backend-run frontend-run proto-gen migrate-up migrate-down sqlc-gen test lint check-cluster dev-all setup fix-keycloak

help: ## Show available commands
	@echo dev-up          Start PostgreSQL, Keycloak, and Adminer (DB UI)
	@echo dev-down        Stop development dependencies
	@echo dev-logs        Tail dependency logs
	@echo db-ui           Open Adminer URL hint for IDP Postgres
	@echo fix-keycloak    Fix Keycloak so Admin-UI users can log into IDP
	@echo check-cluster   Verify minikube/kubernetes is reachable
	@echo backend-run     Run the Go backend server
	@echo frontend-run    Run the Svelte frontend dev server
	@echo dev-all         Run backend and frontend together (two processes)
	@echo setup           Bootstrap env files, deps, DB, and check cluster
	@echo proto-gen       Generate Connect RPC code from protobuf
	@echo sqlc-gen        Generate type-safe SQL code
	@echo migrate-up      Apply database migrations
	@echo migrate-down    Rollback last database migration
	@echo test            Run all tests
	@echo lint            Run linters
	@echo build-backend   Build backend binary
	@echo build-frontend  Build frontend for production
	@echo setup           Bootstrap local development environment

dev-up: ## Start PostgreSQL, Keycloak, and Adminer
	docker compose -f deploy/docker-compose.yml up -d

dev-down: ## Stop development dependencies
	docker compose -f deploy/docker-compose.yml down

dev-logs: ## Tail dependency logs
	docker compose -f deploy/docker-compose.yml logs -f

db-ui: ## Print Adminer login details for the IDP database
	@echo "Adminer:  http://localhost:8081"
	@echo "System:   PostgreSQL"
	@echo "Server:   postgres"
	@echo "Username: idp"
	@echo "Password: idp_dev_password"
	@echo "Database: idp"
	@echo ""
	@echo "IDP tables: projects, project_members, namespaces,"
	@echo "            registry_credentials, git_repositories, builds, audit_logs"
	@echo "(Keycloak also stores its tables in the same database — leave those alone.)"

fix-keycloak: ## Fix Keycloak so Admin-UI users can log into the IDP
	@bash scripts/fix-keycloak-login.sh

backend-run: ## Run the Go backend server
	cd backend && go run ./cmd/server

frontend-run: ## Run the Svelte frontend dev server
	cd frontend && npm run dev

proto-gen: ## Generate Connect RPC code from protobuf
	cd proto && $(BUF) generate

migrate-up: ## Apply database migrations
	cd backend && $(GOOSE) -dir migrations postgres "$(DATABASE_URL)" up

migrate-down: ## Rollback last database migration
	cd backend && $(GOOSE) -dir migrations postgres "$(DATABASE_URL)" down

sqlc-gen: ## Generate type-safe SQL code
	cd backend && $(SQLC) generate

test: ## Run all tests
	cd backend && go test ./...
	cd frontend && npm run test

lint: ## Run linters
	cd backend && golangci-lint run ./...
	cd frontend && npm run lint

build-backend: ## Build backend binary
	cd backend && go build -o ../bin/idp-server ./cmd/server

build-frontend: ## Build frontend for production
	cd frontend && npm run build

setup: dev-up ## Bootstrap local development environment
	@test -f backend/.env || cp backend/.env.example backend/.env
	@test -f frontend/.env || cp frontend/.env.example frontend/.env
	cd frontend && npm ci
	$(MAKE) migrate-up
	@echo ""
	@echo "Next: ensure Docker Desktop is running, then: make check-cluster"
	@echo "Then in two terminals: make backend-run   and   make frontend-run"

check-cluster: ## Verify minikube/kubernetes is reachable
	@minikube status || (echo "Run: minikube start" && exit 1)
	@kubectl get nodes
	@kubectl get ns idp-builds 2>/dev/null || kubectl create namespace idp-builds

ingress-hosts: ## Add *.idp.local entries to /etc/hosts for minikube tunnel
	@bash scripts/setup-ingress-hosts.sh

ingress-proxy: ## Forward Ingress to localhost:18080 (no sudo; keep terminal open)
	@echo "Open http://user-web.user-mgmt.idp.local:18080/  (Ctrl+C to stop)"
	kubectl -n ingress-nginx port-forward svc/ingress-nginx-controller 18080:80

dev-all: ## Run backend and frontend (requires two terminals — use setup first)
	@echo "Run in separate terminals:"
	@echo "  make backend-run"
	@echo "  make frontend-run"
