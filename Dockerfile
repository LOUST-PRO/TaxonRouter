# TaxonRouter — Dual-binary GitHub automation toolkit
//
// This Dockerfile builds both binaries using a multi-stage build.
// The final image is distroless/static for minimal attack surface.

FROM golang:1.26-alpine AS builder

WORKDIR /build

# Install certificates for HTTPS calls.
RUN apk add --no-cache ca-certificates

# Download deps (layer cached).
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build both binaries.
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o taxonrouter-mcp ./cmd/taxonrouter-mcp && \
    CGO_ENABLED=0 go build -ldflags="-s -w" -o taxonrouter-auto-tagger ./cmd/taxonrouter-auto-tagger

# Final image — distroless/static has no shell, no package manager.
FROM gcr.io/distroless/static:nonroot

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /build/taxonrouter-mcp /usr/local/bin/
COPY --from=builder /build/taxonrouter-auto-tagger /usr/local/bin/

USER nonroot

ENTRYPOINT ["/usr/local/bin/taxonrouter-auto-tagger"]
