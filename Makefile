SHELL := /bin/sh

TEMPL := go run github.com/a-h/templ/cmd/templ@v0.3.924
SQLC := go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.29.0
GOOSE := go run github.com/pressly/goose/v3/cmd/goose@v3.24.3 -dir db/migrations

.PHONY: setup generate assets build test fmt lint migrate rollback fixtures audit reconcile restore-drill run worker

setup:
	go mod download
	npm ci --no-audit --no-fund

generate:
	$(TEMPL) generate
	$(SQLC) generate

assets:
	npm run build

build: generate assets
	mkdir -p bin
	go build -mod=mod -o bin/web ./cmd/web
	go build -mod=mod -o bin/worker ./cmd/worker

test: generate assets
	go test -mod=mod -race ./...

fmt:
	gofmt -w cmd internal/config internal/finance internal/http internal/moderation internal/orders internal/platform internal/reviews internal/seller web/static
	$(TEMPL) fmt web/templates

lint: generate assets
	test -z "$$(gofmt -l cmd internal/config internal/finance internal/http internal/moderation internal/orders internal/platform internal/reviews internal/seller web/static)"
	go vet -mod=mod ./...

migrate:
	GOOSE_DRIVER=postgres GOOSE_DBSTRING="$${DATABASE_URL:?set DATABASE_URL}" $(GOOSE) up

rollback:
	GOOSE_DRIVER=postgres GOOSE_DBSTRING="$${DATABASE_URL:?set DATABASE_URL}" $(GOOSE) down

fixtures:
	psql "$${DATABASE_URL:?set DATABASE_URL}" -X -v ON_ERROR_STOP=1 -f db/dev-fixtures.sql

audit: generate assets
	go run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...
	npm audit --audit-level=high

reconcile:
	psql "$${DATABASE_URL:?set DATABASE_URL}" -X -v ON_ERROR_STOP=1 -f scripts/check-finance.sql

restore-drill:
	bash scripts/restore-drill.sh

run: build
	./bin/web

worker: build
	./bin/worker
