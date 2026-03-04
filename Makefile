.PHONY: dev build test vet lint swagger clean docker

# Local development with hot-reload
dev:
	air

# Build the binary
build:
	go build -o ./tmp/main ./cmd/main.go

# Run tests with race detector
test:
	go test -race -v ./...

# Static analysis
vet:
	go vet ./...

# Generate Swagger docs (requires: go install github.com/swaggo/swag/cmd/swag@latest)
swagger:
	swag init -g cmd/main.go -o docs --parseDependency --parseInternal

# Build Docker image
docker:
	docker build -t queuebuzz:latest .

# Clean build artifacts
clean:
	rm -rf tmp/ docs/
