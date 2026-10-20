# SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

SHELL := /bin/bash
MISE ?= mise exec --
LAB := deploy/lab/lab.sh
PROFILES ?= alpha
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X github.com/brokerium/brokerium/internal/version.Version=$(VERSION) \
	-X github.com/brokerium/brokerium/internal/version.Commit=$(COMMIT) \
	-X github.com/brokerium/brokerium/internal/version.Date=$(DATE)
IMAGE_REPO ?= bvaliev
# community, or enterprise for Brokerium Enterprise (ADR 0016).
EDITION ?= community
ifeq ($(filter $(EDITION),community enterprise),)
$(error EDITION must be community or enterprise)
endif
GO_TAGS := $(if $(filter enterprise,$(EDITION)),-tags enterprise)
SERVER_NAME := $(if $(filter enterprise,$(EDITION)),brokerium-server-enterprise,brokerium-server)

.PHONY: help build build-go web-build web-dev dev test test-go test-web lint proto e2e screenshots images \
	lab-up lab-down lab-reset lab-certs lab-ps lab-quorum lab-load licenses

help: ## Show targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-12s %s\n", $$1, $$2}'

build: web-build build-go ## Build the dashboard and both binaries into bin/ (EDITION=enterprise for Enterprise)

build-go:
	$(MISE) go build -trimpath $(GO_TAGS) -ldflags '$(LDFLAGS)' -o bin/$(SERVER_NAME) ./cmd/brokerium-server
	$(MISE) go build -trimpath -ldflags '$(LDFLAGS)' -o bin/brokerium-agent ./cmd/brokerium-agent

web-build: ## Build the dashboard of EDITION into internal/webui/dist
	cd web && $(MISE) pnpm install --frozen-lockfile && VITE_EDITION=$(EDITION) $(MISE) pnpm build && \
		$(MISE) node scripts/check-edition.mjs $(EDITION) dist
	find internal/webui/dist -mindepth 1 ! -name .gitkeep -delete
	cp -R web/dist/. internal/webui/dist/

web-dev: ## Vite dev server with API proxy to localhost:8080
	cd web && VITE_EDITION=$(EDITION) $(MISE) pnpm dev

dev: ## Run the server on the host against the lab, with Docker clusters
	BROKERIUM_ADMIN_PASSWORD=$${BROKERIUM_ADMIN_PASSWORD:-brokerium-admin} \
		BROKERIUM_DOCKER_PROVIDER=$${BROKERIUM_DOCKER_PROVIDER:-on} $(MISE) go run $(GO_TAGS) ./cmd/brokerium-server

test: test-go test-web ## Unit tests

test-go:
	$(MISE) go test ./...
	$(MISE) go test -tags enterprise,licensetest ./...

test-web:
	cd web && $(MISE) pnpm test

lint: ## golangci-lint for both editions, buf lint and web typecheck
	$(MISE) golangci-lint run ./...
	$(MISE) golangci-lint run --build-tags enterprise,licensetest ./...
	$(MISE) buf lint
	cd web && $(MISE) pnpm typecheck

proto: ## Regenerate the agent protocol code
	$(MISE) buf generate

licenses: ## Check dependency licenses against the allowlist
	$(MISE) go run github.com/google/go-licenses@v1.6.0 check ./... \
		--allowed_licenses=Apache-2.0,MIT,BSD-2-Clause,BSD-3-Clause,ISC,MPL-2.0 \
		--ignore github.com/brokerium/brokerium
	cd web && $(MISE) node scripts/check-licenses.mjs

e2e: ## End-to-end scenarios against a running lab and server
	$(MISE) go test -tags e2e -count=1 -timeout 60m ./test/e2e/...

screenshots: build ## Capture dashboard screenshots into docs/images from the lab with demo data
	cd web && $(MISE) pnpm exec playwright install chromium
	deploy/lab/screenshots.sh

images: ## Build container images locally (the server image of EDITION)
	docker build -f deploy/docker/server.Dockerfile --build-arg VERSION=$(VERSION) --build-arg EDITION=$(EDITION) \
		-t $(IMAGE_REPO)/$(SERVER_NAME):$(VERSION) .
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
