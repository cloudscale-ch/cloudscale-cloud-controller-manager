.PHONY: build lint test integration coverage

build:
	go build -trimpath -o bin/cloudscale-cloud-controller-manager \
		cmd/cloudscale-cloud-controller-manager/main.go

lint:
	golangci-lint run --timeout=10m
	staticcheck ./...

test:
	go test -race -coverpkg=./pkg/cloudscale_ccm -coverprofile cover.out ./pkg/cloudscale_ccm -v

integration:
	K8TEST_PATH=${PWD}/k8test go test -count=1 -tags=integration ./pkg/internal/integration -v

coverage: test
	go tool cover -html=cover.out
