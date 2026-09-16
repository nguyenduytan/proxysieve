GO ?= go
PNPM ?= pnpm

.PHONY: bootstrap fmt lint test race fuzz integration browser benchmark build web run docker
bootstrap:
	$(GO) version
	node --version
	$(PNPM) --version
	$(PNPM) --dir web install --frozen-lockfile
	$(PNPM) --dir integrations install --frozen-lockfile

fmt:
	gofmt -w cmd internal pkg
	$(PNPM) --dir web format
	$(PNPM) --dir web exec prettier --write ../integrations

lint:
	$(GO) vet ./...
	golangci-lint run
	$(PNPM) --dir web lint
	$(PNPM) --dir web exec prettier --check ../integrations

test:
	$(GO) test ./...
	$(PNPM) --dir web test
	$(PNPM) --dir integrations test

race:
	$(GO) test -race ./...

fuzz:
	$(GO) test -run '^$$' -fuzz '^FuzzParse$$' -fuzztime=10s -parallel=4 ./pkg/proxy
	$(GO) test -run '^$$' -fuzz '^FuzzPolicyJSON$$' -fuzztime=10s -parallel=4 ./pkg/policy
	$(GO) test -run '^$$' -fuzz '^FuzzLoad$$' -fuzztime=10s -parallel=4 ./internal/configload
	$(GO) test -run '^$$' -fuzz '^FuzzRedactURL$$' -fuzztime=10s -parallel=4 ./internal/security
	$(GO) test -run '^$$' -fuzz '^FuzzConnectTarget$$' -fuzztime=10s -parallel=4 ./internal/transport/httpforward
	$(GO) test -run '^$$' -fuzz '^FuzzSOCKSRequest$$' -fuzztime=10s -parallel=4 ./internal/transport/socks5

integration:
	$(GO) test -count=1 ./internal/app ./internal/upstream ./internal/transport/httpforward ./internal/transport/socks5

browser:
	$(PNPM) --dir integrations test
	$(PNPM) --dir integrations test:e2e

benchmark:
	$(GO) test -run '^$$' -bench 'Benchmark(PolicyEvaluation|Selector|TrafficRecordFullBuffer|ResponseCacheHit)$$' -benchmem ./pkg/policy ./pkg/routing ./internal/traffic ./internal/cache

build:
	$(GO) build -trimpath -o bin/ ./cmd/proxysieve

web:
	$(PNPM) --dir web build

run:
	$(GO) run ./cmd/proxysieve version

docker:
	docker build -t proxysieve:dev .
