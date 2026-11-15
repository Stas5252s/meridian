SHELL := /bin/bash

ROOT := $(shell pwd)

GO ?= go
HELM ?= helm
KUBECTL ?= kubectl
TERRAFORM ?= terraform
KYVERNO ?= kyverno
K6 ?= k6
GOLANGCI_LINT ?= golangci-lint

.PHONY: help test test-race lint helm-lint render policies terraform-check ci \
	up down smoke load port-forward image-bump-staging

help:
	@echo "Meridian targets:"
	@echo "  test            run Go unit tests"
	@echo "  test-race       run Go tests with the race detector"
	@echo "  lint            run golangci-lint"
	@echo "  helm-lint       lint and render the chart for every environment"
	@echo "  policies        run Kyverno CEL policy tests"
	@echo "  terraform-check terraform fmt/validate"
	@echo "  ci              everything CI runs"
	@echo "  up              create kind cluster + ArgoCD + GitOps tree"
	@echo "  down            destroy the local cluster"
	@echo "  smoke           API smoke test"
	@echo "  load            k6 load test against staging"
	@echo "  port-forward    forward ArgoCD/Grafana/Prometheus"

test:
	cd services/checkout-api && CGO_ENABLED=0 $(GO) test ./...

test-race:
	cd services/checkout-api && $(GO) test -race ./...

lint:
	cd services/checkout-api && $(GOLANGCI_LINT) run ./...

helm-lint:
	$(HELM) lint --strict deploy/charts/checkout-api
	@for env in local staging prod; do \
		echo "rendering $$env"; \
		$(HELM) template checkout-api deploy/charts/checkout-api \
			-f deploy/charts/checkout-api/environments/$$env.yaml >/dev/null; \
	done

render:
	$(HELM) template checkout-api deploy/charts/checkout-api \
		-f deploy/charts/checkout-api/environments/staging.yaml

policies:
	$(KYVERNO) test policies/tests
	$(KYVERNO) test policies/tests -f mutate-test.yaml

terraform-check:
	cd infra/terraform && $(TERRAFORM) fmt -check -recursive
	cd infra/terraform && $(TERRAFORM) init -backend=false -input=false >/dev/null
	cd infra/terraform && $(TERRAFORM) validate

ci: test lint helm-lint policies terraform-check

up:
	./scripts/bootstrap.sh

down:
	cd infra/terraform && $(TERRAFORM) destroy -auto-approve

smoke:
	./scripts/smoke.sh

load:
	BASE_URL=$${BASE_URL:-https://checkout-staging.localtest.me:8443} INSECURE_SKIP_TLS=$${INSECURE_SKIP_TLS:-true} $(K6) run scripts/load-test.js

port-forward:
	./scripts/port-forward.sh

image-bump-staging:
	@test -n "$(TAG)" || (echo "usage: make image-bump-staging TAG=v0.2.0" >&2; exit 2)
	./scripts/image-bump.sh $(TAG) staging
