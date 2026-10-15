# SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

SHELL := /bin/bash
MISE ?= mise exec --
LAB := deploy/lab/lab.sh
PROFILES ?= alpha
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X github.com/bvaliev/brokerium/internal/version.Version=$(VERSION) \
	-X github.com/bvaliev/brokerium/internal/version.Commit=$(COMMIT) \
	-X github.com/bvaliev/brokerium/internal/version.Date=$(DATE)
IMAGE_REPO ?= bvaliev

.PHONY: help build build-go web-build web-dev dev test test-go test-web lint proto e2e images \
	lab-up lab-down lab-reset lab-certs lab-ps lab-quorum lab-load licenses

help: ## Show targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-12s %s\n", $$1, $$2}'

build: web-build build-go ## Build the dashboard and both binaries into bin/

build-go:
	$(MISE) go build -trimpath -ldflags '$(LDFLAGS)' -o bin/brokerium-server ./cmd/brokerium-server
	$(MISE) go build -trimpath -ldflags '$(LDFLAGS)' -o bin/brokerium-agent ./cmd/brokerium-agent

web-build: ## Build the dashboard into internal/webui/dist
	cd web && $(MISE) pnpm install --frozen-lockfile && $(MISE) pnpm build
	find internal/webui/dist -mindepth 1 ! -name .gitkeep -delete
	cp -R web/dist/. internal/webui/dist/

web-dev: ## Vite dev server with API proxy to localhost:8080
	cd web && $(MISE) pnpm dev

dev: ## Run the server on the host against the lab
	BROKERIUM_ADMIN_PASSWORD=$${BROKERIUM_ADMIN_PASSWORD:-brokerium-admin} $(MISE) go run ./cmd/brokerium-server

test: test-go test-web ## Unit tests

test-go:
	$(MISE) go test ./...

test-web:
	cd web && $(MISE) pnpm test

lint: ## golangci-lint, buf lint and web typecheck
	$(MISE) golangci-lint run ./...
	$(MISE) buf lint
	cd web && $(MISE) pnpm typecheck

proto: ## Regenerate the agent protocol code
	$(MISE) buf generate

licenses: ## Check dependency licenses against the allowlist
	$(MISE) go run github.com/google/go-licenses@v1.6.0 check ./... \
		--allowed_licenses=Apache-2.0,MIT,BSD-2-Clause,BSD-3-Clause,ISC,MPL-2.0 \
		--ignore github.com/bvaliev/brokerium
	cd web && $(MISE) node scripts/check-licenses.mjs

e2e: ## End-to-end scenarios against a running lab and server
	$(MISE) go test -tags e2e -count=1 -timeout 60m ./test/e2e/...

images: ## Build container images locally
	docker build -f deploy/docker/server.Dockerfile --build-arg VERSION=$(VERSION) -t $(IMAGE_REPO)/brokerium-server:$(VERSION) .
	docker build -f deploy/docker/agent.Dockerfile --build-arg VERSION=$(VERSION) -t $(IMAGE_REPO)/brokerium-agent:$(VERSION) .
	docker build -f deploy/docker/agent-lab.Dockerfile --build-arg VERSION=$(VERSION) -t $(IMAGE_REPO)/brokerium-agent:lab .

lab-up: ## Start PostgreSQL and lab profiles (PROFILES="alpha beta gamma")
	$(LAB) up $(PROFILES)

lab-down: ## Stop the lab, keep volumes
	$(LAB) down

lab-reset: ## Stop the lab and delete all volumes
	$(LAB) reset

lab-certs: ## Generate the throwaway CA and certificates for the TLS listeners of beta
	$(MISE) go run ./deploy/lab/gencerts -out deploy/lab/.certs

lab-ps:
	$(LAB) ps

lab-quorum: ## Describe the quorum of CLUSTER (default alpha)
	$(LAB) quorum $(or $(CLUSTER),alpha)

lab-load: ## Create demo topics and produce records on CLUSTER (default alpha)
	$(LAB) load $(or $(CLUSTER),alpha)
