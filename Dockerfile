FROM golang:1.27.1 AS builder

WORKDIR /src

# Copy go.mod/go.sum first for better layer caching
COPY go.mod go.sum ./
RUN go mod download

COPY Makefile ./
COPY pkg/ pkg/
COPY cmd/ cmd/

ARG VERSION=v0.0.0-dev
ARG GIT_COMMIT
ARG BUILD_DATE

# Convert build args to environment variables for make
ENV VERSION=${VERSION}
ENV GIT_COMMIT=${GIT_COMMIT}
ENV BUILD_DATE=${BUILD_DATE}

RUN make build

FROM alpine:3.23.6
RUN apk add --no-cache ca-certificates

COPY --from=builder /src/bin/cloudscale-cloud-controller-manager /usr/local/bin/cloudscale-cloud-controller-manager
ENTRYPOINT ["cloudscale-cloud-controller-manager"]
