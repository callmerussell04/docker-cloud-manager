# Match canonical local checks to the exact module patch, even on a newer host Go.
export GOTOOLCHAIN := go$(shell awk '$$1 == "go" { print $$2 }' go.mod)
export GOCACHE ?= /tmp/go-build-cache

APP_NAME = docker-cloud-manager
MAIN_PATH = ./cmd/...
BUILD_DIR = ./bin
GOLANGCI_LINT_VERSION = v2.11.4
GOLANGCI_LINT = $(abspath $(BUILD_DIR)/tools/golangci-lint-$(GOLANGCI_LINT_VERSION)/golangci-lint)
GOTESTSUM ?= go run gotest.tools/gotestsum@v1.13.0
MOCKERY_VERSION = v2.53.6
PROTOC ?= protoc
PROTOC_VERSION = libprotoc 34.1
PROTOC_GEN_GO_VERSION = v1.36.11
PROTOC_GEN_GO_GRPC_VERSION = v1.6.2
PROTOC_GEN_GO = $(abspath $(BUILD_DIR)/tools/protobuf-$(PROTOC_GEN_GO_VERSION)/protoc-gen-go)
PROTOC_GEN_GO_GRPC = $(abspath $(BUILD_DIR)/tools/grpc-$(PROTOC_GEN_GO_GRPC_VERSION)/protoc-gen-go-grpc)
PROTO_OUT ?= .
PROTO_FILES = api/core/core.proto api/sso/sso.proto
PROTO_GO_FILES = $(PROTO_FILES:.proto=.pb.go) $(PROTO_FILES:.proto=_grpc.pb.go)
GOVULNCHECK_VERSION = v1.9.0
GOVULNCHECK = $(abspath $(BUILD_DIR)/tools/govulncheck-$(GOVULNCHECK_VERSION)/govulncheck)
TEST_PKGS ?= ./...
TEST_ARGS ?= -count=1 -timeout=5m
TEST_ALL_ARGS ?= -count=1 -race -timeout=5m
TEST_COMPOSE_FILE ?= docker-compose.test.yml
TEST_COMPOSE_PROJECT ?= dcm-test
TEST_KEEP_ENV ?= 0

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
DCM_CLICKHOUSE_TEST_USER ?= dcm_reports_test
DCM_CLICKHOUSE_TEST_PASSWORD ?= clickhouse

.PHONY: all
all: build

.PHONY: build
build:
	CGO_ENABLED=0 go build -mod=readonly -trimpath -buildvcs=false -o $(BUILD_DIR)/ $(MAIN_PATH)

.PHONY: run
run:
	docker compose -f ../docker-compose.yml --env-file .env up -d --build

# Throwaway certificates for a personal machine; needs sudo. Must match REGISTRY_CERTS_DIR in the Compose env file.
REGISTRY_CERTS_DIR ?= /etc/dcm/registry

.PHONY: dev-certs
dev-certs:
	sudo sh ../deployment/registry/dev-certs.sh '$(REGISTRY_CERTS_DIR)'

.PHONY: tidy
tidy:
	go fmt ./...
	go mod tidy

.PHONY: generate generate-mocks generate-proto check-generated check-proto
generate: generate-mocks generate-proto

# Mocks are ignored generated output; clearing them first drops mocks of interfaces no longer listed.
generate-mocks:
	rm -rf internal/testutil/mocks
	GOCACHE=$(GOCACHE) go run github.com/vektra/mockery/v2@$(MOCKERY_VERSION) --config .mockery.yml --quiet

$(PROTOC_GEN_GO):
	GOCACHE=$(GOCACHE) GOBIN=$(dir $(PROTOC_GEN_GO)) go install google.golang.org/protobuf/cmd/protoc-gen-go@$(PROTOC_GEN_GO_VERSION)

$(PROTOC_GEN_GO_GRPC):
	GOCACHE=$(GOCACHE) GOBIN=$(dir $(PROTOC_GEN_GO_GRPC)) go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@$(PROTOC_GEN_GO_GRPC_VERSION)

generate-proto: $(PROTOC_GEN_GO) $(PROTOC_GEN_GO_GRPC)
	@test "$$($(PROTOC) --version)" = "$(PROTOC_VERSION)" || { printf '%s\n' 'Install protoc 34.1 (protobuf release v34.1), or set PROTOC to that binary.' >&2; exit 1; }
	$(PROTOC) --plugin=protoc-gen-go=$(PROTOC_GEN_GO) --plugin=protoc-gen-go-grpc=$(PROTOC_GEN_GO_GRPC) \
		--go_out=$(PROTO_OUT) --go_opt=paths=source_relative \
		--go-grpc_out=$(PROTO_OUT) --go-grpc_opt=paths=source_relative $(PROTO_FILES)

check-generated: generate-mocks check-proto

check-proto:
	@set -eu; \
	generated_dir=$$(mktemp -d); \
	trap 'rm -rf "$$generated_dir"' EXIT; \
	$(MAKE) --no-print-directory generate-proto PROTO_OUT="$$generated_dir"; \
	for file in $(PROTO_GO_FILES); do \
		diff -u "$$file" "$$generated_dir/$$file" || { printf '%s\n' 'Protobuf drift detected; run make generate-proto.' >&2; exit 1; }; \
	done

.PHONY: test-fast
test-fast: check-generated
	env -u DCM_TEST_REQUIRE_INTEGRATION \
		-u CORE_TEST_DATABASE_URL \
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
		-u CORE_TEST_REGISTRY_ISOLATION \
		-u CORE_TEST_NETWORK_POLICY \
		GOCACHE=$(GOCACHE) $(GOTESTSUM) --format pkgname -- $(TEST_ARGS) $(TEST_PKGS)

.PHONY: test-env-up
test-env-up:
	DCM_CLICKHOUSE_TEST_DB='$(DCM_CLICKHOUSE_TEST_DB)' \
	DCM_CLICKHOUSE_TEST_USER='$(DCM_CLICKHOUSE_TEST_USER)' \
	DCM_CLICKHOUSE_TEST_PASSWORD='$(DCM_CLICKHOUSE_TEST_PASSWORD)' \
	docker compose -f $(TEST_COMPOSE_FILE) -p $(TEST_COMPOSE_PROJECT) up -d --wait

.PHONY: test-env-logs
test-env-logs:
	docker compose -f $(TEST_COMPOSE_FILE) -p $(TEST_COMPOSE_PROJECT) ps --all
	docker compose -f $(TEST_COMPOSE_FILE) -p $(TEST_COMPOSE_PROJECT) logs --no-color --timestamps

.PHONY: test-env-down
test-env-down:
	docker compose -f $(TEST_COMPOSE_FILE) -p $(TEST_COMPOSE_PROJECT) down -v --remove-orphans

# Every integration gate is required: a missing service fails instead of skipping.
.PHONY: test test-all
test test-all: check-generated
	set -e; \
	export DCM_CLICKHOUSE_TEST_DB='$(DCM_CLICKHOUSE_TEST_DB)' DCM_CLICKHOUSE_TEST_USER='$(DCM_CLICKHOUSE_TEST_USER)' DCM_CLICKHOUSE_TEST_PASSWORD='$(DCM_CLICKHOUSE_TEST_PASSWORD)'; \
	if [ "$(TEST_KEEP_ENV)" != "1" ]; then trap 'docker compose -f $(TEST_COMPOSE_FILE) -p $(TEST_COMPOSE_PROJECT) down -v --remove-orphans' EXIT; fi; \
	docker compose -f $(TEST_COMPOSE_FILE) -p $(TEST_COMPOSE_PROJECT) up -d --wait --wait-timeout 120; \
	DCM_TEST_REQUIRE_INTEGRATION=1 \
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
	DCM_CLICKHOUSE_TEST_USER='$(DCM_CLICKHOUSE_TEST_USER)' \
	DCM_CLICKHOUSE_TEST_PASSWORD='$(DCM_CLICKHOUSE_TEST_PASSWORD)' \
	CORE_TEST_DOCKER=1 \
	BUILDER_TEST_DOCKER=1 \
	TELEMETRY_TEST_DOCKER=1 \
	CORE_TEST_REGISTRY_ISOLATION=1 \
	GOCACHE=$(GOCACHE) $(GOTESTSUM) --format pkgname -- $(TEST_ALL_ARGS) $(TEST_PKGS)

$(GOLANGCI_LINT):
	GOBIN=$(dir $(GOLANGCI_LINT)) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

.PHONY: lint
lint: check-generated $(GOLANGCI_LINT)
	$(GOLANGCI_LINT) run ./... --config=./.golangci.yml

.PHONY: check-format check-tidy vet test-race vulncheck verify
check-format:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then printf 'Unformatted Go files; run gofmt -w:\n%s\n' "$$unformatted" >&2; exit 1; fi

# Tidiness covers test imports, so the ignored mocks must exist first.
check-tidy: check-generated
	@go mod tidy -diff || { printf '%s\n' 'go.mod/go.sum are not tidy; run go mod tidy.' >&2; exit 1; }

vet: check-generated
	go vet ./...

test-race:
	$(MAKE) test-fast TEST_ARGS='$(TEST_ARGS) -race'

$(GOVULNCHECK):
	GOBIN=$(dir $(GOVULNCHECK)) go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)

vulncheck: build $(GOVULNCHECK)
	./vulncheck/check.sh $(GOVULNCHECK) $(BUILD_DIR) builder core gateway sso telemetry

# Integration-free gates in failure-cost order; make test and make vulncheck run separately.
verify: check-format check-generated check-tidy build vet lint test-race
