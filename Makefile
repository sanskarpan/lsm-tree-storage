.PHONY: run test test-race bench lint clean loadtest backup docker-build compose-up compose-down

run:
	go run ./cmd/server

test:
	go test ./...

test-race:
	go test ./... -race -count=3

bench:
	go test ./... -bench=. -benchmem -run='^$$'

lint:
	golangci-lint run ./...

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
	rm -f *.sst *.log
