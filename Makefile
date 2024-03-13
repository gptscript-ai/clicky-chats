GO_TAGS ?= netgo
build:
	CGO_ENABLED=0 go build -o bin/clicky-chats -tags "${GO_TAGS}" -ldflags "-s -w" .

tidy:
	go mod tidy

image:
	docker build .

GOLANGCI_LINT_VERSION ?= v1.55.2
setup-env:
	if ! command -v golangci-lint &> /dev/null; then \
  		echo "Could not find golangci-lint, installing version $(GOLANGCI_LINT_VERSION)."; \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $$(go env GOPATH)/bin $(GOLANGCI_LINT_VERSION); \
	fi

lint: setup-env
	golangci-lint run

# Runs linters and validates that all generated code is committed.
validate-code: tidy lint
	if [ -n "$$(git status --porcelain)" ]; then \
		git status --porcelain; \
		echo "Encountered dirty repo!"; \
		git diff; \
		exit 1 \
	;fi

GOTESTSUM_VERSION ?= v1.10.0
GOTESTSUM ?= go run gotest.tools/gotestsum@$(GOTESTSUM_VERSION) --format testname $(TEST_FLAGS) -- $(GO_TEST_FLAGS)

.PHONY: test unit integration
test: unit integration

unit:
	$(GOTESTSUM) $$(go list ./... | grep -v /integration/)

integration:
	$(GOTESTSUM) ./integration/...

generate:
	go generate ./pkg/generated/generate.go

run-dev:
	go run -tags "${GO_TAGS}" -ldflags "-s -w" ./main.go server --with-agents=true