# Build stage
FROM golang:1.23-alpine AS builder

# Install protobuf compiler and plugins
RUN apk add --no-cache protobuf protobuf-dev && \
    go install google.golang.org/protobuf/cmd/protoc-gen-go@latest && \
    go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

WORKDIR /build

# Copy go mod files
COPY go.mod ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Generate protobuf code
RUN protoc --go_out=. --go_opt=paths=source_relative \
    --go-grpc_out=. --go-grpc_opt=paths=source_relative \
    proto/security.proto

# Generate go.sum with all dependencies including generated files
RUN go mod tidy

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o talos-csr-signer ./cmd

# Runtime stage
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /

# Copy binary from builder
COPY --from=builder /build/talos-csr-signer /talos-csr-signer

# Use nonroot user
USER 65532:65532

EXPOSE 50001

ENTRYPOINT ["/talos-csr-signer"]
