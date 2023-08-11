.PHONY: build lint test

build:
	go build -trimpath -o bin/cloudscale-cloud-controller-manager \
		cmd/cloudscale-cloud-controller-manager/main.go

lint:
	golangci-lint run

test:
	go test -race -coverpkg=./pkg/... ./pkg/...
