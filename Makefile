.PHONY: build lint test

build:
	go build -trimpath -o bin/cloudscale-cloud-controller-manager \
		cmd/cloudscale-cloud-controller-manager/main.go

lint:
	golangci-lint run
	staticcheck ./...

test:
	go test -race -coverpkg=./pkg/... -coverprofile cover.out ./pkg/...

coverage: test
	go tool cover -html=cover.out
