FROM golang:1.20-alpine AS build
ARG VERSION

RUN apk add --no-cache git

WORKDIR /host
COPY . /host

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -trimpath -ldflags "-s -w -X main.version=$VERSION" \
  -o builds/cloudscale-cloud-controller-manager \
  cmd/cloudscale-cloud-controller-manager/main.go

FROM alpine:latest
RUN apk add --no-cache ca-certificates

COPY --from=build /host/builds/cloudscale-cloud-controller-manager /usr/local/bin/cloudscale-cloud-controller-manager
ENTRYPOINT ["cloudscale-cloud-controller-manager"] 
