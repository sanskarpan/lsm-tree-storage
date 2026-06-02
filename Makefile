.PHONY: run build test test-race test-coverage vet lint bench loadtest backup docker-build compose-up compose-down clean

GO ?= go
GOLANGCI_LINT ?= golangci-lint

run:
	$(GO) run ./cmd/server

build:
	$(GO) build ./...

test:
	$(GO) test ./...

test-race:
	$(GO) test ./... -race -count=3

test-coverage:
	$(GO) test ./... -coverprofile=coverage.out -covermode=atomic
	$(GO) tool cover -func=coverage.out | tail -1

vet:
	$(GO) vet ./...

# golangci-lint is optional; if it isn't installed fall back to `go vet`.
# `go vet` already catches the most common correctness issues.
lint:
	@if command -v $(GOLANGCI_LINT) >/dev/null 2>&1; then \
		$(GOLANGCI_LINT) run ./...; \
	else \
		echo "golangci-lint not installed; falling back to go vet"; \
		$(GO) vet ./...; \
	fi

bench:
	$(GO) test ./... -bench=. -benchmem -run='^$$'

loadtest:
	./scripts/loadtest.sh

backup:
	./scripts/backup.sh ./data ./backups

docker-build:
	docker build -f Dockerfile.backend -t lsm-backend:local .
	docker build -f frontend/Dockerfile -t lsm-frontend:local ./frontend

compose-up:
	docker compose up --build

compose-down:
	docker compose down

clean:
	rm -rf data/
	rm -f *.sst *.log coverage.out
