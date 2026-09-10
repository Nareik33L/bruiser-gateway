GO        ?= go
PKG       := ./...
BIN       := bin/bruiser
SIMTIX    := bin/simtix
DATABASE_URL ?= postgres://bruiser:bruiser@127.0.0.1:5432/bruiser?sslmode=disable
TEST_DATABASE_URL ?= postgres://bruiser:bruiser@127.0.0.1:5432/bruiser_test?sslmode=disable

.PHONY: all build test test-race lint fmt vet serve migrate tidy ci torture simtix authority-check check-demo-boundary demo-build demo-up-local demo-down-local demo-reset demo-check demo-nuke demo-e2e

all: build

tidy:
	$(GO) mod tidy

build:
	mkdir -p bin
	$(GO) build -o $(BIN) ./cmd/bruiser
	$(GO) build -o $(SIMTIX) ./cmd/simtix

demo-build:
	mkdir -p bin
	$(GO) build -o $(BIN) ./cmd/bruiser
	$(GO) build -o bin/harchester ./demos/harchester-web
	$(GO) build -o bin/simtix-demo ./demos/simtix
	$(GO) build -o bin/admin ./demos/admin-console
	$(GO) build -o bin/loadlab ./demos/load-lab

check-demo-boundary:
	bash scripts/check-demo-boundary.sh

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
	$(BIN) authority-check --edge http://127.0.0.1:8091 --origin http://127.0.0.1:8090

demo-check: demo-build
	$(BIN) authority-check --edge http://127.0.0.1:8091 --origin http://127.0.0.1:8090 \
		--membership 1001234 --event hfc-ars --json

demo-reset:
	curl -sS -X POST http://127.0.0.1:8110/reset -H 'Cookie: admin_session=harchester-ok' || true

demo-up-local: demo-build
	bash scripts/demo-up-local.sh

demo-e2e:
	DEMO_E2E=1 BRUISER_BIN="$(CURDIR)/bin/bruiser" BRUISER_TEST_DATABASE_URL="$(TEST_DATABASE_URL)" $(GO) test ./demos/e2e -count=1 -timeout 180s

demo-down-local:
	bash scripts/demo-down-local.sh

demo-nuke:
	@echo "Refusing to drop databases from Make. Use psql DROP DATABASE harchester / bruiser if you mean it."

migrate: build
	BRUISER_DATABASE_URL="$(DATABASE_URL)" $(BIN) migrate

ci: vet test-race build check-demo-boundary demo-build
