GO        ?= go
PKG       := ./...
BIN       := bin/bruiser
SIMTIX    := bin/simtix
DATABASE_URL ?= postgres://bruiser:bruiser@127.0.0.1:5432/bruiser?sslmode=disable
TEST_DATABASE_URL ?= postgres://bruiser:bruiser@127.0.0.1:5432/bruiser_test?sslmode=disable

# Lab posture for make serve / simtix / migrate. Production is the default
# when BRUISER_ENV is unset; these targets must ask for lab explicitly.
LAB_ENV ?= BRUISER_ENV=lab \
	BRUISER_ADMIN_SECRET=admin-secret-dev \
	BRUISER_OPERATOR_SECRET=operator-secret-dev \
	BRUISER_ADMIN_ADDR=127.0.0.1:8082 \
	BRUISER_EDGE_SECRET=edge-secret-dev \
	BRUISER_ORIGIN_SECRET=origin-lock-dev \
	BRUISER_DEV_HMAC_SECRET=dev-secret-change-me \
	BRUISER_PROFILE=configs/arsenal.yaml

.PHONY: all build test test-race lint fmt vet serve migrate tidy ci torture simtix authority-check eaf-demo eaf-nightly sdk-test doctor \
	demo-1x10000 demo-1000x10 demo-handoff demo-bypass demo-unaware demo-up v1-accept soak

all: build

tidy:
	$(GO) mod tidy

build:
	mkdir -p bin
	$(GO) build -o $(BIN) ./cmd/bruiser
	$(GO) build -o $(SIMTIX) ./cmd/simtix
	$(GO) build -o bin/swarm ./cmd/swarm

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
	$(LAB_ENV) BRUISER_DATABASE_URL="$(DATABASE_URL)" \
	BRUISER_PROXY_ADDR=":8081" BRUISER_ORIGIN_URL="http://127.0.0.1:8090" \
	$(BIN) serve

simtix: build
	$(LAB_ENV) SIMTIX_HTTP_ADDR=":8090" SIMTIX_EDGE_ADDR=":8091" \
	SIMTIX_ORIGIN_SECRET="origin-lock-dev" \
	SIMTIX_BRUISER_URL="http://127.0.0.1:8080" \
	SIMTIX_ORIGIN_URL="http://127.0.0.1:8090" \
	BRUISER_MERCHANT_ID="arsenal" \
	$(SIMTIX)

authority-check: build
	$(LAB_ENV) $(BIN) authority-check --front http://127.0.0.1:8091 --origin http://127.0.0.1:8090 --control http://127.0.0.1:8080 --admin http://127.0.0.1:8082

doctor: build
	$(LAB_ENV) $(BIN) doctor --profile configs/example.yaml --skip-store

config-validate: build
	$(LAB_ENV) $(BIN) config validate configs/example.yaml

eaf-demo: build
	$(BIN) eaf-demo --front http://127.0.0.1:8091 --n 2000

eaf-nightly: build
	BRUISER_TEST_DATABASE_URL="$(TEST_DATABASE_URL)" BRUISER_EAF_N=10000 \
		$(GO) test ./internal/check -count=1 -timeout 10m -run TestEAFNightly

# Long churn: 10,000 agents, 30 minutes, renew/reconnect/release/queue.
# Override with BRUISER_SOAK_DURATION=60m BRUISER_SOAK_AGENTS=10000.
soak:
	BRUISER_TEST_DATABASE_URL="$(TEST_DATABASE_URL)" \
	BRUISER_SOAK_DURATION="$${BRUISER_SOAK_DURATION:-30m}" \
	BRUISER_SOAK_AGENTS="$${BRUISER_SOAK_AGENTS:-10000}" \
		$(GO) test ./internal/torture -count=1 -timeout 90m -run '^TestSoakChurn$$' -v

v1-accept:
	BRUISER_TEST_DATABASE_URL="$(TEST_DATABASE_URL)" $(GO) test ./internal/check ./internal/api/public ./internal/store/postgres ./internal/ops ./internal/simtix \
		-count=1 -timeout 15m \
		-run 'TestV1|TestAuthority|TestEAFUnaware|TestEmbedded|TestProgressive|TestDryRun|TestHTTP|TestConcurrent|TestFail|TestSecurity|TestPersist|TestEmergency|TestMetrics|TestOneHundred|TestAdminLogin|TestDuplicate|TestExpiry'

sdk-test:
	@command -v node >/dev/null && (cd sdk/node && node --test test.js) || echo "node not installed; skipped"
	@command -v python3 >/dev/null && python3 -c "import cryptography" 2>/dev/null \
		&& (cd sdk/python && python3 -m unittest tests/test_protect.py) \
		|| echo "python cryptography not installed; skipped"

demo-up:
	docker compose -f deploy/compose/docker-compose.yml --profile demo up --build -d

demo-1x10000: build
	$(BIN) swarm --front http://127.0.0.1:8081 --profile 1xN --n 10000

demo-1000x10: build
	$(BIN) swarm --front http://127.0.0.1:8081 --profile NxK --customers 1000 --per-customer 10

demo-handoff: build
	$(BIN) swarm --profile handoff

demo-bypass: build
	$(BIN) swarm --front http://127.0.0.1:8090 --origin http://127.0.0.1:8090 --profile bypass --n 20

demo-unaware: build
	$(BIN) swarm --front http://127.0.0.1:8091 --profile unaware --n 200

migrate: build
	$(LAB_ENV) BRUISER_DATABASE_URL="$(DATABASE_URL)" $(BIN) migrate

ci: vet test-race build sdk-test
