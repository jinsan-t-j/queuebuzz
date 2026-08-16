.PHONY: dev build test vet lint swagger clean docker hooks lint-fix sonar-audit

# SonarQube audit
sonar-audit:
	./scripts/sonar-audit.sh


# Install/update git hooks (Husky equivalent for Go)
hooks:
	go run github.com/evilmartians/lefthook@latest install

# Install the linter
install-lint:
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Detailed code analysis (checks local bin or system path)
lint-fix:
	@if [ -f "./bin/golangci-lint" ]; then ./bin/golangci-lint run --fix; else golangci-lint run --fix; fi

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
	@if command -v swag >/dev/null 2>&1; then \
		swag init -g main.go -o docs --parseDependency --parseInternal; \
	elif [ -f "$$(go env GOPATH)/bin/swag" ]; then \
		"$$(go env GOPATH)/bin/swag" init -g main.go -o docs --parseDependency --parseInternal; \
	elif [ -f "./bin/swag" ]; then \
		./bin/swag init -g main.go -o docs --parseDependency --parseInternal; \
	else \
		echo "swag not found. Install it with: go install github.com/swaggo/swag/cmd/swag@latest"; \
		exit 1; \
	fi

# Build Docker image
docker:
	docker build -t queuebuzz:latest .

# Clean build artifacts
clean:
	rm -rf tmp/ docs/
