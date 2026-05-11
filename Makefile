APP := herdlite
BIN_DIR := bin
MAIN := ./cmd/herdlite
GO_ENV := GOCACHE=$(CURDIR)/.cache/go-build

.PHONY: build run test fmt vet release clean

build:
	@./scripts/build.sh

run:
	@$(GO_ENV) go run $(MAIN)

test:
	@$(GO_ENV) go test ./...

fmt:
	@gofmt -w cmd internal

vet:
	@$(GO_ENV) go vet ./...

release:
	@./scripts/release.sh

clean:
	@rm -rf $(BIN_DIR) .cache
