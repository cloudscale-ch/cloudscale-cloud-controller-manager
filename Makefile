.PHONY: build lint test

build:
	go build -trimpath -o builds/cloudscale-cloud-controller-manager \
		cmd/cloudscale-cloud-controller-manager/main.go

lint:
	golangci-lint run

test:
	go test -coverpkg=./pkg/... ./pkg/...
