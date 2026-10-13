SHELL := /bin/bash
GO ?= go
MODULES := modules/platform modules/commerce apps/api
export GOTOOLCHAIN := go1.26.9

.PHONY: run test vet fmt fmt-check tidy build check boundaries smoke test-deploy migrate seed smoke-persistence

run:
	@set -a; if [ -f .env ]; then source .env; fi; set +a; $(GO) run ./apps/api/cmd/api

migrate:
	@set -a; if [ -f .env ]; then source .env; fi; set +a; $(GO) run ./apps/api/cmd/db migrate

seed:
	@set -a; if [ -f .env ]; then source .env; fi; set +a; $(GO) run ./apps/api/cmd/db seed

test:
	@set -e; for module in $(MODULES); do (cd $$module && GOWORK=off $(GO) test -race -count=1 ./...); done

vet:
	@set -e; for module in $(MODULES); do (cd $$module && GOWORK=off $(GO) vet ./...); done

fmt:
	@gofmt -w $$(find apps modules -name '*.go')

fmt-check:
	@test -z "$$(gofmt -l $$(find apps modules -name '*.go'))" || (echo 'Run make fmt'; exit 1)

tidy:
	@set -e; for module in $(MODULES); do (cd $$module && GOWORK=off $(GO) mod tidy); done

build:
	@mkdir -p bin
	@cd apps/api && GOWORK=off $(GO) build -trimpath -o ../../bin/api ./cmd/api
	@cd apps/api && GOWORK=off $(GO) build -trimpath -o ../../bin/db ./cmd/db

boundaries:
	@GO=$(GO) bash scripts/check-boundaries.sh

check: fmt-check boundaries vet test build

smoke:
	@bash scripts/smoke.sh

test-deploy:
	@bash scripts/test-deploy.sh

smoke-persistence:
	@python3 scripts/smoke-persistence.py
