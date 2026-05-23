.DEFAULT_GOAL := help
SHELL         := /usr/bin/env bash

BINARY := standardizer
BIN_DIR := bin

.PHONY: help build test lint vet fmt fmt-check clean security \
        secrets-scan-staged lefthook-bootstrap lefthook-install hooks setup

help: ## Show available targets
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z_-]+:.*##/ {printf "\033[36m%-22s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build the standardizer binary
	go build -o $(BIN_DIR)/$(BINARY) ./cmd/standardizer

test: ## Run all unit tests with race detection
	go test -race -shuffle=on ./...

vet: ## Run go vet
	go vet ./...

lint: vet ## Run go vet + golangci-lint
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run ./... || \
	  echo "golangci-lint not found; skipping (install from https://golangci-lint.run)"

fmt: ## Format all Go source files
	gofmt -w .

fmt-check: ## Check Go formatting without modifications
	@diff=$$(gofmt -l .); if [ -n "$$diff" ]; then \
	  printf "Unformatted files:\n%s\n\nRun 'make fmt' to fix.\n" "$$diff"; exit 1; fi

security: ## Run govulncheck (dependency vulnerability scan)
	@command -v govulncheck >/dev/null 2>&1 && govulncheck ./... || \
	  echo "govulncheck not found; install with: go install golang.org/x/vuln/cmd/govulncheck@latest"

clean: ## Remove build artifacts
	rm -rf $(BIN_DIR) output/

secrets-scan-staged: ## Scan staged files for secrets
	@command -v gitleaks >/dev/null 2>&1 || { \
		echo "ERROR: gitleaks not found. Install from https://github.com/gitleaks/gitleaks#installing"; exit 1; }
	gitleaks protect --staged --redact


PLATFORM_STANDARDS_SHA := 3f7326412e455e6ec3b1ab6f5b721ff071c6254c
PLATFORM_STANDARDS_RAW := https://raw.githubusercontent.com/FelipeFuhr/ffreis-platform-standards

HOOK_SCRIPTS := \
	check_merge_markers.sh \
	check_large_files.sh \
	check_binary_files.sh \
	check_commit_msg.sh \
	check_required_tools.sh

hook-scripts: ## Download bootstrap + hook scripts from ffreis-platform-standards
	@mkdir -p scripts/hooks
	@curl -fsSL "$(PLATFORM_STANDARDS_RAW)/$(PLATFORM_STANDARDS_SHA)/lefthook/bootstrap_lefthook.sh" \
		-o scripts/bootstrap_lefthook.sh && chmod +x scripts/bootstrap_lefthook.sh
	@for script in $(HOOK_SCRIPTS); do \
		curl -fsSL "$(PLATFORM_STANDARDS_RAW)/$(PLATFORM_STANDARDS_SHA)/lefthook/scripts/$$script" \
			-o "scripts/hooks/$$script" && chmod +x "scripts/hooks/$$script"; \
	done
	@echo "Hook scripts downloaded."

lefthook-bootstrap: hook-scripts ## Download lefthook binary to .bin/
	bash ./scripts/bootstrap_lefthook.sh

lefthook-install: ## Install git hooks via lefthook
	lefthook install

hooks: lefthook-bootstrap lefthook-install ## Bootstrap and install all git hooks

setup: hooks ## Install hooks and verify required tools
	@command -v gitleaks >/dev/null 2>&1 || { \
		echo ""; echo "ACTION REQUIRED: gitleaks is not installed."; \
		echo "Install from https://github.com/gitleaks/gitleaks#installing then re-run 'make setup'."; \
		echo ""; exit 1; }
	@echo "Dev environment ready."
