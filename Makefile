# go-service-template Makefile. `make help` lists targets.

COMPOSE := docker compose -f deploy/compose/docker-compose.yaml
COVERAGE_OUT := out/coverage.out
BIN := bin/server
SWAG := go run github.com/swaggo/swag/cmd/swag@v1.16.6
SWAGGER_DIR := api/swagger
IMAGE := ghcr.io/biairmal/go-service-template
GIT_SHA := $(shell git rev-parse --short HEAD)
IDENTITY_SRC ?= ../go-identity
# The dev service-client secret; identity.yaml holds its argon2id hash.
IDENTITY_DEV_SECRET := local-note-service-secret

.DEFAULT_GOAL := help
.PHONY: help up up-obs up-identity down image run build swagger test test-integration lint format format-check coverage vulncheck check \
	migrate-up migrate-down migrate-create

help: ## Show targets
	@grep -E "^[a-z-]+:.*## " Makefile | sed -e "s/:.*## /\t/"

up: ## Build the image and start the stack: Postgres, migrate, api, worker, Caddy (https://localhost), backup
	$(COMPOSE) up -d --build --wait

up-obs: ## make up plus Prometheus, Grafana (localhost:3000), Loki and Tempo, with tracing on
	TRACING_ENABLED=true $(COMPOSE) --profile obs up -d --build --wait

up-identity: ## make up plus go-identity on localhost:8081 built from IDENTITY_SRC (../go-identity); auth on
	test -f $(IDENTITY_SRC)/keys/keys.yaml || $(MAKE) -C $(IDENTITY_SRC) keys
	IDENTITY_SRC="$(abspath $(IDENTITY_SRC))" AUTH_ENABLED=true IDENTITY_CLIENT_SECRET=$(IDENTITY_DEV_SECRET) \
		$(COMPOSE) --profile identity up -d --build --wait

down: ## Stop the stack (every profile)
	$(COMPOSE) --profile obs --profile identity down

image: swagger ## Generate Swagger docs, then build the image tagged with the git SHA
	docker build --build-arg SERVICE_VERSION=$(GIT_SHA) -t $(IMAGE):$(GIT_SHA) .

swagger: ## Generate Swagger docs from handler annotations into api/swagger
	$(SWAG) init -g cmd/server/main.go -o $(SWAGGER_DIR) --packageName swagger --outputTypes go,json --parseDependency --parseInternal

build: swagger ## Generate Swagger docs, then build bin/server
	go build -o $(BIN) ./cmd/server

run: swagger ## Generate Swagger docs, then run the API against the local stack
	go run ./cmd/server serve

test: ## Unit tests
	go test -short ./...

test-integration: ## Unit + integration tests (Docker needed for Testcontainers)
	go test ./...

lint: ## golangci-lint
	golangci-lint run ./...

format: ## gofumpt
	gofumpt -w .

format-check: ## Fail when a file is not gofumpt-formatted
	@test -z "$$(gofumpt -l .)" || (gofumpt -l . && exit 1)

coverage: ## Unit-test coverage into out/
	@mkdir -p out
	go test -short -coverprofile=$(COVERAGE_OUT) -covermode=atomic ./...
	go tool cover -func=$(COVERAGE_OUT)

vulncheck: ## govulncheck
	govulncheck ./...

check: format-check lint test coverage vulncheck ## CI gate: format, lint, unit tests, coverage, vulncheck

migrate-up: ## Apply migrations
	go run ./cmd/server migrate up

migrate-down: ## Revert the last migration
	go run ./cmd/server migrate down

migrate-create: ## New migration: make migrate-create name=add_x
	go run ./cmd/server migrate create $(name)
