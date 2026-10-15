# SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

SHELL := /bin/bash
MISE ?= mise exec --
LAB := deploy/lab/lab.sh
PROFILES ?= alpha

.PHONY: lab-up lab-down lab-reset lab-ps lab-quorum lab-load

lab-up: ## Start PostgreSQL and lab profiles (PROFILES="alpha beta gamma")
	$(LAB) up $(PROFILES)

lab-down: ## Stop the lab, keep volumes
	$(LAB) down

lab-reset: ## Stop the lab and delete all volumes
	$(LAB) reset

lab-ps:
	$(LAB) ps

lab-quorum: ## Describe the quorum of CLUSTER (default alpha)
	$(LAB) quorum $(or $(CLUSTER),alpha)

lab-load: ## Create demo topics and produce records on CLUSTER (default alpha)
	$(LAB) load $(or $(CLUSTER),alpha)
