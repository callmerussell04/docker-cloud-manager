APP_NAME = docker-cloud-manager
MAIN_PATH = ./cmd/...
BUILD_DIR = ./bin
GOLANGCI_LINT = $(BUILD_DIR)/golangci-lint
GOLANGCI_LINT_VERSION = v2.11.4

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

.install-linter:
	[ -f $(GOLANGCI_LINT) ] || curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(BUILD_DIR) $(GOLANGCI_LINT_VERSION)

.PHONY: lint
lint: .install-linter
	$(GOLANGCI_LINT) run ./... --config=./.golangci.yml