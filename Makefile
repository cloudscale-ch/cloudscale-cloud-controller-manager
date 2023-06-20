.PHONY: build lint

build:
	go build -trimpath -o builds/cloudscale-cloud-controller-manager \
		cmd/cloudscale-cloud-controller-manager/main.go

lint:
	golangci-lint run
