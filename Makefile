.PHONY: build lint test

build:
	go build -trimpath -o bin/cloudscale-cloud-controller-manager \
		cmd/cloudscale-cloud-controller-manager/main.go

lint:
	golangci-lint run --timeout=10m
	staticcheck ./...

test:
	go test -race -coverpkg=./pkg/cloudscale_ccm -coverprofile cover.out ./pkg/cloudscale_ccm

coverage: test
	go tool cover -html=cover.out
