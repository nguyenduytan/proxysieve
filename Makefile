GO ?= go
PNPM ?= pnpm

.PHONY: bootstrap fmt lint test race build web run docker
bootstrap:
	$(GO) version
	node --version
	$(PNPM) --version
	$(PNPM) --dir web install --frozen-lockfile

fmt:
	gofmt -w cmd internal
	$(PNPM) --dir web format

lint:
	$(GO) vet ./...
	golangci-lint run
	$(PNPM) --dir web lint

test:
	$(GO) test ./...
	$(PNPM) --dir web test

race:
	$(GO) test -race ./...

build:
	$(GO) build -trimpath -o bin/ ./cmd/proxysieve

web:
	$(PNPM) --dir web build

run:
	$(GO) run ./cmd/proxysieve version

docker:
	docker build -t proxysieve:dev .
