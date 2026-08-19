GO ?= go
BIN_DIR ?= bin
PACKAGES := ./...
CONFIG_PACKAGES := ./internal/config ./internal/configstore ./internal/revision
CONTROL_PACKAGES := ./internal/control ./internal/controltransport ./internal/controltoken
TARGETS := darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64
FUZZ_TIME ?= 10s
export GOTOOLCHAIN ?= local

.PHONY: fmt-check vet test test-race config-test config-race fault-test fuzz-smoke control-test control-race control-fault control-fuzz control-ci build cross-build verify ci clean

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

config-test:
	$(GO) test $(CONFIG_PACKAGES) -count=100

config-race:
	$(GO) test -race $(CONFIG_PACKAGES)

fault-test:
	$(GO) test -tags=fault ./internal/configstore -run 'TestWriteAtomicInjectedStages|TestAtomicWriteCrashRecovery' -count=10
	$(GO) test -tags=stress ./internal/config -run TestManagerConcurrentReadersStress -count=3

fuzz-smoke:
	$(GO) test ./internal/config -run=^$$ -fuzz=FuzzDecodeCanonicalRoundTrip -fuzztime=$(FUZZ_TIME)
	$(GO) test ./internal/configstore -run=^$$ -fuzz=FuzzManifestRoundTrip -fuzztime=$(FUZZ_TIME)
	$(GO) test ./internal/revision -run=^$$ -fuzz=FuzzIDValidation -fuzztime=$(FUZZ_TIME)

control-test:
	$(GO) test $(CONTROL_PACKAGES) -count=100

control-race:
	$(GO) test -race $(CONTROL_PACKAGES)

control-fault:
	$(GO) test ./internal/control -run 'Test(RecoverPanicsDiscardsPartialResponse|ServerForcedShutdownWithActiveRequest|ClientProtocolFailures|LimitListener)' -count=10
	$(GO) test ./internal/controltransport -run 'TestUnixLiveAndStaleSocketHandling|TestUnixCloseDoesNotRemoveReplacementSocket' -count=10
	$(GO) test -tags=controlstress ./internal/control -run TestAuthenticatedServerStress -count=3

control-fuzz:
	$(GO) test ./internal/control -run=^$$ -fuzz=FuzzAuthorizationHeader -fuzztime=$(FUZZ_TIME)
	$(GO) test ./internal/control -run=^$$ -fuzz=FuzzValidRequestID -fuzztime=$(FUZZ_TIME)
	$(GO) test ./internal/control -run=^$$ -fuzz=FuzzStrictControlJSON -fuzztime=$(FUZZ_TIME)
	$(GO) test ./internal/controltransport -run=^$$ -fuzz=FuzzParseInstanceName -fuzztime=$(FUZZ_TIME)
	$(GO) test ./internal/controltoken -run=^$$ -fuzz=FuzzParseToken -fuzztime=$(FUZZ_TIME)

control-ci: control-test control-race control-fault control-fuzz

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

ci: verify config-test fault-test fuzz-smoke control-test control-fault

clean:
	rm -rf $(BIN_DIR)
