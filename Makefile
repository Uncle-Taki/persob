.PHONY: all build test test-race lint run docker-up docker-down clean

BINARY_NAME=perso

all: lint test build

build:
	@mkdir -p bin
	go build -ldflags="-s -w" -o bin/$(BINARY_NAME) ./cmd/perso

test:
	go test -v ./...

test-race:
	go test -race -v ./...

lint:
	gofmt -s -w .
	go vet ./...

run: build
	./bin/$(BINARY_NAME) all

migrate: build
	./bin/$(BINARY_NAME) migrate

docker-up:
	docker compose up --build

docker-down:
	docker compose down -v

clean:
	rm -rf bin/
