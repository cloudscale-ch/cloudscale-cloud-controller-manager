# Build stage
FROM golang:1.26-alpine AS build

RUN apk add --no-cache git

WORKDIR /build

# Copy go.mod and go.sum first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the http-echo binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -o /http-echo ./

# Production stage - distroless static
FROM gcr.io/distroless/static-debian11

LABEL org.opencontainers.image.description="Internal test helper for CCM integration tests. Not for general use."

COPY --from=build /http-echo /http-echo

ENTRYPOINT ["/http-echo"]
