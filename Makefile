GO ?= go
BIN_DIR ?= bin
PACKAGES := ./...
CONFIG_PACKAGES := ./internal/config ./internal/configstore ./internal/revision
CONTROL_PACKAGES := ./internal/control ./internal/controltransport ./internal/controltoken
PHASE1C_PACKAGES := ./internal/configservice ./internal/runtimepaths ./cmd/routerd ./cmd/routerctl
CANONICAL_PACKAGES := ./internal/canonical ./internal/canonicaljson
STREAMING_PACKAGES := ./internal/streaming
TARGETS := darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64
FUZZ_TIME ?= 10s
export GOTOOLCHAIN ?= local

.PHONY: fmt-check vet test test-race config-test config-race fault-test fuzz-smoke control-test control-race control-fault control-fuzz control-ci configservice-test configservice-race cli-test cli-race phase1c-fault phase1c-e2e phase1c-fuzz phase1c-ci canonical-test canonical-race canonical-golden canonical-stress canonical-fuzz phase2a-ci streaming-test streaming-race streaming-golden streaming-stress streaming-fuzz phase2b-ci build cross-build verify ci clean

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

configservice-test:
	$(GO) test ./internal/configservice -count=100

configservice-race:
	$(GO) test -race ./internal/configservice

cli-test:
	$(GO) test ./cmd/routerd ./cmd/routerctl -count=100

cli-race:
	$(GO) test -race ./cmd/routerd ./cmd/routerctl

phase1c-fault:
	$(GO) test ./internal/configservice -run 'TestConcurrentApplyOneGenerationConflict|TestApplyRepairsIdenticalCorruptRevision' -count=10
	$(GO) test ./internal/daemon -run 'TestDaemonBootstrapApplyAndRestart|TestDaemonEndpointCollision' -count=10

phase1c-e2e:
	$(GO) test -tags=e2e ./test/e2e -count=1

phase1c-fuzz:
	$(GO) test ./internal/configservice -run=^$$ -fuzz=FuzzValidateConfiguration -fuzztime=$(FUZZ_TIME)
	$(GO) test ./internal/control -run=^$$ -fuzz=FuzzConfigRequestJSON -fuzztime=$(FUZZ_TIME)
	$(GO) test ./cmd/routerctl -run=^$$ -fuzz=FuzzParseCLI -fuzztime=$(FUZZ_TIME)

phase1c-ci: configservice-test configservice-race cli-test cli-race phase1c-fault phase1c-e2e phase1c-fuzz

canonical-test:
	$(GO) test $(CANONICAL_PACKAGES) -count=100

canonical-race:
	$(GO) test -race $(CANONICAL_PACKAGES)

canonical-golden:
	$(GO) test ./internal/canonical -run TestGoldenFixtures -count=10

canonical-stress:
	$(GO) test -tags=canonicalstress ./internal/canonical -run TestCanonicalConcurrentReadStress -count=3

canonical-fuzz:
	$(GO) test ./internal/canonicaljson -run=^$$ -fuzz=FuzzParseRoundTrip -fuzztime=$(FUZZ_TIME)
	$(GO) test ./internal/canonical -run=^$$ -fuzz=FuzzParseRequestID -fuzztime=$(FUZZ_TIME)
	$(GO) test ./internal/canonical -run=^$$ -fuzz=FuzzRemoteImageURL -fuzztime=$(FUZZ_TIME)
	$(GO) test ./internal/canonical -run=^$$ -fuzz=FuzzToolName -fuzztime=$(FUZZ_TIME)
	$(GO) test ./internal/canonical -run=^$$ -fuzz=FuzzTextDelta -fuzztime=$(FUZZ_TIME)
	$(GO) test ./internal/canonical -run=^$$ -fuzz=FuzzContinuation -fuzztime=$(FUZZ_TIME)
	$(GO) test ./internal/canonical -run=^$$ -fuzz=FuzzDecodeRequest -fuzztime=$(FUZZ_TIME)
	$(GO) test ./internal/canonical -run=^$$ -fuzz=FuzzDecodeResponse -fuzztime=$(FUZZ_TIME)
	$(GO) test ./internal/canonical -run=^$$ -fuzz=FuzzDecodeEvent -fuzztime=$(FUZZ_TIME)

phase2a-ci: canonical-test canonical-race canonical-golden canonical-stress canonical-fuzz

streaming-test:
	$(GO) test $(STREAMING_PACKAGES) -count=100

streaming-race:
	$(GO) test -race $(STREAMING_PACKAGES)

streaming-golden:
	$(GO) test $(STREAMING_PACKAGES) -run '^TestMachineReconstructsGoldenStream$$' -count=10

streaming-stress:
	$(GO) test -tags=streamingstress $(STREAMING_PACKAGES) -run '^TestConcurrentIndependentStreamStress$$' -count=3

streaming-fuzz:
	$(GO) test $(STREAMING_PACKAGES) -run=^$$ -fuzz=FuzzMachineEventStream -fuzztime=$(FUZZ_TIME)
	$(GO) test $(STREAMING_PACKAGES) -run=^$$ -fuzz=FuzzPumpFaultScript -fuzztime=$(FUZZ_TIME)

phase2b-ci: streaming-test streaming-race streaming-golden streaming-stress

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

ci: verify config-test fault-test fuzz-smoke control-test control-fault configservice-test cli-test phase1c-fault canonical-test canonical-golden canonical-stress streaming-test streaming-golden streaming-stress

clean:
	rm -rf $(BIN_DIR)
