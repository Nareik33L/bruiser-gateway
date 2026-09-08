GO        ?= go
PKG       := ./...
BIN       := bin/bruiser
DATABASE_URL ?= postgres://bruiser:bruiser@127.0.0.1:5432/bruiser?sslmode=disable
TEST_DATABASE_URL ?= postgres://bruiser:bruiser@127.0.0.1:5432/bruiser_test?sslmode=disable

.PHONY: all build test test-race lint fmt vet serve migrate tidy ci

all: build

tidy:
	$(GO) mod tidy

build:
	mkdir -p bin
	$(GO) build -o $(BIN) ./cmd/bruiser

test:
	BRUISER_TEST_DATABASE_URL="$(TEST_DATABASE_URL)" $(GO) test $(PKG) -count=1

test-race:
	BRUISER_TEST_DATABASE_URL="$(TEST_DATABASE_URL)" $(GO) test -race $(PKG) -count=1

vet:
	$(GO) vet $(PKG)

fmt:
	$(GO) fmt $(PKG)

lint:
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run || echo "golangci-lint not installed; skipped"

serve: build
	BRUISER_DATABASE_URL="$(DATABASE_URL)" $(BIN) serve

migrate: build
	BRUISER_DATABASE_URL="$(DATABASE_URL)" $(BIN) migrate

ci: vet test-race build
