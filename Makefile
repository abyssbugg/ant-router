GO ?= go
BIN_DIR ?= bin
PACKAGES := ./...
TARGETS := darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64

.PHONY: fmt-check vet test test-race build cross-build verify clean

fmt-check:
	@files="$$(gofmt -l cmd internal)"; \
	if [ -n "$$files" ]; then \
		echo "Go files need formatting:"; \
		echo "$$files"; \
		exit 1; \
	fi

vet:
	$(GO) vet $(PACKAGES)

test:
	$(GO) test $(PACKAGES)

test-race:
	$(GO) test -race $(PACKAGES)

build:
	@mkdir -p $(BIN_DIR)
	$(GO) build -o $(BIN_DIR)/routerd ./cmd/routerd
	$(GO) build -o $(BIN_DIR)/routerctl ./cmd/routerctl

cross-build:
	@set -eu; \
	for target in $(TARGETS); do \
		goos=$${target%/*}; \
		goarch=$${target#*/}; \
		ext=""; \
		if [ "$$goos" = "windows" ]; then ext=".exe"; fi; \
		out="$(BIN_DIR)/$$goos-$$goarch"; \
		mkdir -p "$$out"; \
		echo "building $$goos/$$goarch"; \
		CGO_ENABLED=0 GOOS="$$goos" GOARCH="$$goarch" $(GO) build -o "$$out/routerd$$ext" ./cmd/routerd; \
		CGO_ENABLED=0 GOOS="$$goos" GOARCH="$$goarch" $(GO) build -o "$$out/routerctl$$ext" ./cmd/routerctl; \
	done

verify: fmt-check vet test test-race build cross-build

clean:
	rm -rf $(BIN_DIR)
