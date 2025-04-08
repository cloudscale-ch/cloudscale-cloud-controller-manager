.PHONY: build lint test integration coverage

build:
	go build -trimpath -o bin/cloudscale-cloud-controller-manager \
		cmd/cloudscale-cloud-controller-manager/main.go

lint:
	go tool -modfile tool.mod golangci-lint run --timeout=10m --show-stats=false
	go tool -modfile tool.mod staticcheck ./...

test:
	go test -race -v \
		-coverpkg=./pkg/cloudscale_ccm,./pkg/internal/actions,./pkg/internal/compare \
		-coverprofile cover.out \
			./pkg/cloudscale_ccm \
			./pkg/internal/actions \
			./pkg/internal/compare

integration:
	K8TEST_PATH=${PWD}/k8test go test -count=1 -tags=integration ./pkg/internal/integration -v -timeout 30m

coverage: test
	go tool cover -html=cover.out
