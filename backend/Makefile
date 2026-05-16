APP_NAME = docker-cloud-manager
MAIN_PATH = ./cmd/...
BUILD_DIR = ./bin
GOLANGCI_LINT = $(BUILD_DIR)/golangci-lint
GOLANGCI_LINT_VERSION = v2.11.4
GOTESTSUM ?= $(shell command -v gotestsum 2>/dev/null || printf 'go run gotest.tools/gotestsum@latest')
GOCACHE ?= /tmp/go-build-cache
TEST_PKGS ?= ./...
TEST_ARGS ?= -count=1
TEST_ALL_ARGS ?= -count=1 -p=1
TEST_COMPOSE_FILE ?= docker-compose.test.yml
TEST_COMPOSE_PROJECT ?= dcm-test
TEST_KEEP_ENV ?= 0
TEST_DOCKER_SMOKE ?= 1

CORE_TEST_DATABASE_URL ?= postgres://postgres:postgres@127.0.0.1:15432/dcm_core_test?sslmode=disable
SSO_TEST_DATABASE_URL ?= postgres://postgres:postgres@127.0.0.1:15433/dcm_sso_test?sslmode=disable
CORE_TEST_OBJECT_STORAGE_ENDPOINT ?= 127.0.0.1:19000
CORE_TEST_OBJECT_STORAGE_BUCKET ?= dcm-core-test
CORE_TEST_OBJECT_STORAGE_ACCESS_KEY ?= minioadmin
CORE_TEST_OBJECT_STORAGE_SECRET_KEY ?= minioadmin
CORE_TEST_RABBITMQ_URL ?= amqp://guest:guest@127.0.0.1:15692/
BUILDER_TEST_RABBITMQ_URL ?= $(CORE_TEST_RABBITMQ_URL)
DCM_CLICKHOUSE_TEST_ADDR ?= 127.0.0.1:19002
DCM_CLICKHOUSE_TEST_DB ?= dcm_reports_test
DCM_CLICKHOUSE_TEST_PASSWORD ?= clickhouse

.PHONY: all
all: build

.PHONY: build
build: tidy
	go build -o $(BUILD_DIR)/ $(MAIN_PATH)

.PHONY: run
run:
	docker compose up -d --build

.PHONY: tidy
tidy:
	go fmt ./...
	go mod tidy

.PHONY: test-fast
test-fast:
	env -u CORE_TEST_DATABASE_URL \
		-u SSO_TEST_DATABASE_URL \
		-u CORE_TEST_OBJECT_STORAGE_ENDPOINT \
		-u CORE_TEST_OBJECT_STORAGE_BUCKET \
		-u CORE_TEST_OBJECT_STORAGE_ACCESS_KEY \
		-u CORE_TEST_OBJECT_STORAGE_SECRET_KEY \
		-u CORE_TEST_RABBITMQ_URL \
		-u BUILDER_TEST_RABBITMQ_URL \
		-u DCM_CLICKHOUSE_TEST_ADDR \
		-u DCM_CLICKHOUSE_TEST_DB \
		-u DCM_CLICKHOUSE_TEST_USER \
		-u DCM_CLICKHOUSE_TEST_PASSWORD \
		-u CORE_TEST_DOCKER \
		-u BUILDER_TEST_DOCKER \
		-u TELEMETRY_TEST_DOCKER \
		GOCACHE=$(GOCACHE) $(GOTESTSUM) --format pkgname -- $(TEST_ARGS) $(TEST_PKGS)

.PHONY: test-env-up
test-env-up:
	docker compose -f $(TEST_COMPOSE_FILE) -p $(TEST_COMPOSE_PROJECT) up -d --wait

.PHONY: test-env-down
test-env-down:
	docker compose -f $(TEST_COMPOSE_FILE) -p $(TEST_COMPOSE_PROJECT) down -v --remove-orphans

.PHONY: test test-all
test test-all:
	set -e; \
	docker compose -f $(TEST_COMPOSE_FILE) -p $(TEST_COMPOSE_PROJECT) up -d --wait; \
	if [ "$(TEST_KEEP_ENV)" != "1" ]; then trap 'docker compose -f $(TEST_COMPOSE_FILE) -p $(TEST_COMPOSE_PROJECT) down -v --remove-orphans' EXIT; fi; \
	CORE_TEST_DATABASE_URL='$(CORE_TEST_DATABASE_URL)' \
	SSO_TEST_DATABASE_URL='$(SSO_TEST_DATABASE_URL)' \
	CORE_TEST_OBJECT_STORAGE_ENDPOINT='$(CORE_TEST_OBJECT_STORAGE_ENDPOINT)' \
	CORE_TEST_OBJECT_STORAGE_BUCKET='$(CORE_TEST_OBJECT_STORAGE_BUCKET)' \
	CORE_TEST_OBJECT_STORAGE_ACCESS_KEY='$(CORE_TEST_OBJECT_STORAGE_ACCESS_KEY)' \
	CORE_TEST_OBJECT_STORAGE_SECRET_KEY='$(CORE_TEST_OBJECT_STORAGE_SECRET_KEY)' \
	CORE_TEST_RABBITMQ_URL='$(CORE_TEST_RABBITMQ_URL)' \
	BUILDER_TEST_RABBITMQ_URL='$(BUILDER_TEST_RABBITMQ_URL)' \
	DCM_CLICKHOUSE_TEST_ADDR='$(DCM_CLICKHOUSE_TEST_ADDR)' \
	DCM_CLICKHOUSE_TEST_DB='$(DCM_CLICKHOUSE_TEST_DB)' \
	DCM_CLICKHOUSE_TEST_PASSWORD='$(DCM_CLICKHOUSE_TEST_PASSWORD)' \
	CORE_TEST_DOCKER='$(TEST_DOCKER_SMOKE)' \
	BUILDER_TEST_DOCKER='$(TEST_DOCKER_SMOKE)' \
	TELEMETRY_TEST_DOCKER='$(TEST_DOCKER_SMOKE)' \
	GOCACHE=$(GOCACHE) $(GOTESTSUM) --format pkgname -- $(TEST_ALL_ARGS) $(TEST_PKGS)

.install-linter:
	[ -f $(GOLANGCI_LINT) ] || curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(BUILD_DIR) $(GOLANGCI_LINT_VERSION)

.PHONY: lint
lint: .install-linter
	$(GOLANGCI_LINT) run ./... --config=./.golangci.yml
