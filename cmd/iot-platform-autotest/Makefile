.PHONY: help test test-verbose test-coverage test-telemetry test-attribute test-event test-command clean deps init

help:
	@echo "Available commands:"
	@echo "  make deps            - Download dependencies"
	@echo "  make init            - Initialize config file"
	@echo "  make test            - Run all tests"
	@echo "  make test-verbose    - Run tests with verbose output"
	@echo "  make test-coverage   - Run tests with coverage report"
	@echo "  make test-telemetry  - Run telemetry tests only"
	@echo "  make test-attribute  - Run attribute tests only"
	@echo "  make test-event      - Run event tests only"
	@echo "  make test-command    - Run command tests only"
	@echo "  make clean           - Clean test artifacts"

deps:
	go mod download
	go mod tidy

init:
	@if [ ! -f config.yaml ]; then \
		cp config.example.yaml config.yaml; \
		echo "Created config.yaml from template. Please edit it with your settings."; \
	else \
		echo "config.yaml already exists."; \
	fi

test:
	go test ./tests/... -timeout 5m

test-verbose:
	go test ./tests/... -v -timeout 5m

test-coverage:
	go test ./tests/... -v -coverprofile=coverage.out -timeout 5m
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

test-telemetry:
	go test ./tests/telemetry_test.go -v -timeout 3m

test-attribute:
	go test ./tests/attribute_test.go -v -timeout 3m

test-event:
	go test ./tests/event_test.go -v -timeout 3m

test-command:
	go test ./tests/command_test.go -v -timeout 3m

clean:
	rm -f coverage.out coverage.html
	go clean -testcache

fmt:
	go fmt ./...

lint:
	golangci-lint run ./...

build:
	go build -o bin/autotest ./cmd/autotest/

run: build
	./bin/autotest
