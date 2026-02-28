.PHONY: run test lint docker-up docker-down

run:
	go run ./cmd/server

test:
	go test ./...

lint:
	gofmt -w .
	go vet ./...

docker-up:
	docker compose up --build

docker-down:
	docker compose down
