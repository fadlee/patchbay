# Build Stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Download dependencies
COPY go.mod ./
# Download modules if go.sum exists or just verify
RUN if [ -f go.sum ]; then go mod download; fi

# Copy source code
COPY . .
ARG VERSION=1.1.1

# Build static binary for Linux with injected version
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w -X main.Version=${VERSION}" -o patchbay .

# Final Stage
FROM alpine:3.21

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/patchbay /app/patchbay

# Environment variable to allow external dashboard access
ENV PATCHBAY_HOST=0.0.0.0

# Directory for storing config.json
VOLUME ["/app"]

# Expose default dashboard admin port
EXPOSE 8787

ENTRYPOINT ["/app/patchbay"]
