# Image URL to use for building/pushing
TAG ?= dev
IMG ?= quay.io/cloudscalech/cloudscale-cloud-controller-manager:$(TAG)
LDFLAGS ?= -X main.version=$(TAG)

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk command is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: fmt vet ## Run unit tests.
	go test -race -v \
		-coverpkg=./pkg/cloudscale_ccm,./pkg/internal/actions,./pkg/internal/compare \
		-coverprofile cover.out \
		./pkg/cloudscale_ccm \
		./pkg/internal/actions \
		./pkg/internal/compare

.PHONY: lint
lint: golangci-lint ## Run golangci-lint linter
	"$(GOLANGCI_LINT)" run

.PHONY: lint-fix
lint-fix: golangci-lint ## Run golangci-lint linter and perform fixes
	"$(GOLANGCI_LINT)" run --fix

.PHONY: govulncheck
govulncheck: govulncheck-tool ## Run govulncheck to scan for known, reachable vulnerabilities (incl. stdlib/toolchain).
	"$(GOVULNCHECK)" ./...

##@ Build

.PHONY: build
build: fmt vet ## Build CCM binary.
	go build -ldflags '$(LDFLAGS)' -trimpath -o bin/cloudscale-cloud-controller-manager \
		cmd/cloudscale-cloud-controller-manager/main.go

CONTAINER_TOOL ?= docker

.PHONY: docker-build
docker-build: ## Build docker image with the manager.
	$(CONTAINER_TOOL) build --platform linux/amd64 --build-arg VERSION=$(TAG) -t ${IMG} .

.PHONY: docker-push
docker-push: ## Push docker image with the manager.
	$(CONTAINER_TOOL) push ${IMG}

##@ Integration Tests

.PHONY: integration
integration: ## Run integration tests.
	K8TEST_PATH=${PWD}/k8test go test -count=1 -tags=integration ./pkg/internal/integration -v -timeout 30m

##@ Tooling

# Host OS/ARCH used to namespace tool binaries so a shared ./bin (e.g. host tree
# mounted into a Linux sandbox) never mixes incompatible binaries.
HOST_PLATFORM := $(shell go env GOOS)-$(shell go env GOARCH)
## Location to install dependencies to
LOCALBIN := $(shell pwd)/bin/$(HOST_PLATFORM)
$(LOCALBIN):
	mkdir -p "$(LOCALBIN)"

## Tool Binaries
GOLANGCI_LINT = $(LOCALBIN)/golangci-lint
GOVULNCHECK ?= $(LOCALBIN)/govulncheck

## Tool Versions
GOLANGCI_LINT_VERSION ?= v2.13.1
GOVULNCHECK_VERSION ?= v1.7.0

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT): $(LOCALBIN)
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/v2/cmd/golangci-lint,$(GOLANGCI_LINT_VERSION))

.PHONY: govulncheck-tool
govulncheck-tool: $(GOVULNCHECK) ## Download govulncheck locally if necessary.
$(GOVULNCHECK): $(LOCALBIN)
	$(call go-install-tool,$(GOVULNCHECK),golang.org/x/vuln/cmd/govulncheck,$(GOVULNCHECK_VERSION))

# go-install-tool will 'go install' any package with custom target and name of binary, if it doesn't exist
# $1 - target path with name of binary
# $2 - package url which can be installed
# $3 - specific version of package
define go-install-tool
@[ -f "$(1)-$(3)" ] || { \
set -e; \
package=$(2)@$(3) ;\
echo "Downloading $${package}" ;\
rm -f "$(1)" ;\
GOBIN="$(LOCALBIN)" go install $${package} ;\
mv "$(LOCALBIN)/$$(basename "$(1)")" "$(1)-$(3)" ;\
} ;\
ln -sf "$$(realpath "$(1)-$(3)")" "$(1)"
endef
