GO        ?= go
PKG       := ./...
BIN       := bin/bruiser
SIMTIX    := bin/simtix
DATABASE_URL ?= postgres://bruiser:bruiser@127.0.0.1:5432/bruiser?sslmode=disable
TEST_DATABASE_URL ?= postgres://bruiser:bruiser@127.0.0.1:5432/bruiser_test?sslmode=disable

.PHONY: all build test test-race lint fmt vet serve migrate tidy ci torture simtix authority-check eaf-demo

all: build

tidy:
	$(GO) mod tidy

build:
	mkdir -p bin
	$(GO) build -o $(BIN) ./cmd/bruiser
	$(GO) build -o $(SIMTIX) ./cmd/simtix

test:
	BRUISER_TEST_DATABASE_URL="$(TEST_DATABASE_URL)" $(GO) test $(PKG) -count=1

test-race:
	BRUISER_TEST_DATABASE_URL="$(TEST_DATABASE_URL)" $(GO) test -race $(PKG) -count=1

torture: build
	BRUISER_TEST_DATABASE_URL="$(TEST_DATABASE_URL)" $(GO) run ./cmd/torture

vet:
	$(GO) vet $(PKG)

fmt:
	$(GO) fmt $(PKG)

lint:
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run || echo "golangci-lint not installed; skipped"

serve: build
	BRUISER_DATABASE_URL="$(DATABASE_URL)" $(BIN) serve

simtix: build
	SIMTIX_HTTP_ADDR=":8090" SIMTIX_EDGE_ADDR=":8091" \
	SIMTIX_ORIGIN_SECRET="origin-lock-dev" \
	SIMTIX_BRUISER_URL="http://127.0.0.1:8080" \
	$(SIMTIX)

authority-check: build
	$(BIN) authority-check --front http://127.0.0.1:8091 --origin http://127.0.0.1:8090

eaf-demo: build
	$(BIN) eaf-demo --front http://127.0.0.1:8091 --n 2000

migrate: build
	BRUISER_DATABASE_URL="$(DATABASE_URL)" $(BIN) migrate

ci: vet test-race build
