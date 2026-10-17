GO ?= go
BIN_DIR ?= bin
PACKAGES := ./...
CONFIG_PACKAGES := ./internal/config ./internal/configstore ./internal/revision
CONTROL_PACKAGES := ./internal/control ./internal/controltransport ./internal/controltoken
PHASE1C_PACKAGES := ./internal/configservice ./internal/runtimepaths ./cmd/routerd ./cmd/routerctl
CANONICAL_PACKAGES := ./internal/canonical ./internal/canonicaljson
STREAMING_PACKAGES := ./internal/streaming
PHASE3A_PACKAGES := ./internal/openai/responses ./internal/provider/mock ./internal/dataplane ./internal/daemon ./cmd/routerctl
PHASE3B_PACKAGES := ./internal/openai/responses ./internal/provider/mock ./internal/dataplane
PHASE4A_PACKAGES := ./internal/config ./internal/credential ./internal/execution ./internal/provider/openai ./internal/daemon ./internal/control
PHASE4B_PACKAGES := ./internal/canonical ./internal/streaming ./internal/openai/continuation ./internal/openai/responses ./internal/execution ./internal/provider/openai ./internal/dataplane ./internal/daemon
PHASE4C_PACKAGES := ./internal/config ./internal/credential ./internal/execution ./internal/provider/anthropic ./internal/daemon ./internal/control ./internal/dataplane
CODEX_ALPHA_PACKAGES := ./internal/atomicfile ./internal/filelock ./internal/codexcatalog ./internal/codexintegration ./internal/codexownership ./internal/codexprocess ./internal/codexprofile ./internal/codexversion ./internal/control ./internal/canonical ./internal/streaming ./internal/openai/responses ./internal/provider/mock ./internal/daemon ./cmd/routerctl
TARGETS := darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64
FUZZ_TIME ?= 10s
export GOTOOLCHAIN ?= local

.PHONY: phase4a-test phase4a-race phase4a-contract phase4a-fault phase4a-stress phase4a-fuzz phase4a-ci phase4a-live-compile phase4a-live
.PHONY: phase4b-test phase4b-race phase4b-contract phase4b-fault phase4b-stress phase4b-fuzz phase4b-ci phase4b-live-compile
.PHONY: phase4c-test phase4c-race phase4c-contract phase4c-fault phase4c-stress phase4c-fuzz phase4c-e2e phase4c-ci phase4c-live-compile phase4c-live
.PHONY: codex-alpha-test codex-alpha-race codex-alpha-e2e codex-alpha-fuzz codex-alpha-ci
.PHONY: fmt-check vet test test-race cover config-test config-race fault-test fuzz-smoke control-test control-race control-fault control-fuzz control-ci configservice-test configservice-race cli-test cli-race phase1c-fault phase1c-e2e phase1c-fuzz phase1c-ci canonical-test canonical-race canonical-golden canonical-stress canonical-fuzz phase2a-ci streaming-test streaming-race streaming-golden streaming-stress streaming-fuzz phase2b-ci phase3a-test phase3a-race phase3a-fault phase3a-sdk phase3a-stress phase3a-fuzz phase3a-ci phase3b-test phase3b-race phase3b-sdk phase3b-e2e phase3b-stress phase3b-fuzz phase3b-ci build cross-build verify ci clean

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

cover:
	$(GO) test $(PACKAGES) -coverprofile=coverage.out -count=1
	$(GO) tool cover -func=coverage.out | tail -1

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
	$(GO) test ./internal/canonical -run=^$$ -fuzz=^FuzzToolName$$ -fuzztime=$(FUZZ_TIME)
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

phase3a-test:
	$(GO) test $(PHASE3A_PACKAGES) -count=100

phase3a-race:
	$(GO) test -race $(PHASE3A_PACKAGES)

phase3a-fault:
	$(GO) test ./internal/dataplane -run 'Test(DisconnectFailsBeforeAnySSECommit|HandlerContainsPanicsWithoutLeakingValues|HandlerRequiresInferenceTokenAndLoopbackHost)' -count=20

phase3a-sdk:
	$(GO) test ./internal/dataplane -run TestOfficialOpenAISDKModelsAndResponses -count=20
	$(GO) test -tags=e2e ./test/e2e -run TestMockResponsesSubprocessWorkflow -count=3 -timeout=5m

phase3a-stress:
	$(GO) test -race -tags=phase3astress ./internal/dataplane -run TestConcurrentInferenceStress -count=3

phase3a-fuzz:
	$(GO) test ./internal/openai/responses -run=^$$ -fuzz='^FuzzDecodeRequest$$' -fuzztime=$(FUZZ_TIME)
	$(GO) test ./internal/openai/responses -run=^$$ -fuzz='^FuzzDecodeRequestRedactsUnknownValues$$' -fuzztime=$(FUZZ_TIME)
	$(GO) test ./internal/dataplane -run=^$$ -fuzz='^FuzzInferenceBoundary$$' -fuzztime=$(FUZZ_TIME)

phase3a-ci: phase3a-test phase3a-race phase3a-fault phase3a-sdk phase3a-stress

phase3b-test:
	$(GO) test ./internal/openai/responses -run 'Image' -count=100
	$(GO) test ./internal/provider/mock -run 'Vision|Catalog' -count=100
	$(GO) test ./internal/dataplane -run 'Image|MockCatalog' -count=20

phase3b-race:
	$(GO) test -race $(PHASE3B_PACKAGES)

phase3b-sdk:
	$(GO) test ./internal/dataplane -run '^TestOfficialOpenAISDKImageResponses$$' -count=20

phase3b-e2e:
	$(GO) test -tags=e2e ./test/e2e -run '^TestMockResponsesSubprocessWorkflow$$' -count=3

phase3b-stress:
	$(GO) test -race -tags=phase3bstress ./internal/dataplane -run '^TestImageInferenceConcurrentStress$$' -count=3

phase3b-fuzz:
	$(GO) test ./internal/openai/responses -run '^$$' -fuzz='^FuzzDecodeImage$$' -fuzztime=$(FUZZ_TIME)
	$(GO) test ./internal/openai/responses -run '^$$' -fuzz='^FuzzImageDimensions$$' -fuzztime=$(FUZZ_TIME)

phase3b-ci: phase3b-test phase3b-race phase3b-sdk phase3b-e2e phase3b-stress

phase4a-test:
	$(GO) test $(PHASE4A_PACKAGES) -count=100

phase4a-race:
	$(GO) test -race $(PHASE4A_PACKAGES)

phase4a-contract:
	$(GO) test ./internal/provider/openai -run '^TestOpenAIProviderLocalContract' -count=20
	$(GO) test ./internal/daemon -run '^TestDaemonReportsMissingOpenAICredentialAndOmitsItsModels$$' -count=20

phase4a-fault:
	$(GO) test ./internal/provider/openai -run 'Test(OpenAIProviderLocalContractRejectsMalformedAndSequenceGap|OpenAIProviderLocalContractNormalizesHTTPFailuresWithoutRetry|SecureHTTPClientBoundsWireResponse|EventSourceRejectsSequenceGapsAndUnsupportedEvents|EventSourceProtocolFailurePoisonsStream|ProviderNormalizesStarterAndCancellationErrors|NormalizeProviderErrorBoundsRetryAfter)' -count=20

phase4a-stress:
	$(GO) test -race -tags=phase4astress ./internal/provider/openai -run '^TestOpenAIProviderConcurrentStress$$' -count=3

phase4a-fuzz:
	$(GO) test ./internal/provider/openai -run='^$$' -fuzz='^FuzzOpenAIEventSourceRawEvent$$' -fuzztime=$(FUZZ_TIME)

phase4a-ci: phase4a-test phase4a-race phase4a-contract phase4a-fault phase4a-stress phase4a-live-compile

phase4a-live-compile:
	$(GO) test -tags=liveopenai ./internal/provider/openai -run '^$$' -count=1

phase4a-live:
	@test "$${ANT_ROUTER_LIVE_OPENAI:-}" = "1" || { echo "ANT_ROUTER_LIVE_OPENAI=1 is required" >&2; exit 2; }
	@test -n "$${OPENAI_API_KEY:-}" || { echo "OPENAI_API_KEY is required" >&2; exit 2; }
	@test -n "$${ANT_ROUTER_LIVE_OPENAI_MODEL:-}" || { echo "ANT_ROUTER_LIVE_OPENAI_MODEL is required" >&2; exit 2; }
	$(GO) test -tags=liveopenai ./internal/provider/openai -run '^TestLiveOpenAICertification$$' -count=1

phase4b-test:
	$(GO) test $(PHASE4B_PACKAGES) -run 'Continuation|TwoTurn' -count=100

phase4b-race:
	$(GO) test -race $(PHASE4B_PACKAGES) -run 'Continuation|TwoTurn'

phase4b-contract:
	$(GO) test ./internal/provider/openai -run 'TwoTurnStatelessToolContinuation' -count=20

phase4b-fault:
	$(GO) test $(PHASE4B_PACKAGES) -run 'Continuation.*(Rejects|Failure|Missing|Oversized)|AffinityMismatch' -count=20

phase4b-stress:
	$(GO) test -race -tags=phase4bstress ./internal/provider/openai -run '^TestOpenAIContinuationReplayConcurrentStress$$' -count=3

phase4b-fuzz:
	$(GO) test ./internal/openai/continuation -run='^$$' -fuzz='^FuzzContinuationEnvelope$$' -fuzztime=$(FUZZ_TIME)
	$(GO) test ./internal/openai/responses -run='^$$' -fuzz='^FuzzDecodeContinuationReplay$$' -fuzztime=$(FUZZ_TIME)

phase4b-live-compile:
	$(GO) test -tags=liveopenai ./internal/provider/openai -run '^$$' -count=1

phase4b-ci: phase4b-test phase4b-race phase4b-contract phase4b-fault phase4b-stress phase4b-live-compile

phase4c-test:
	$(GO) test $(PHASE4C_PACKAGES) -count=100

phase4c-race:
	$(GO) test -race $(PHASE4C_PACKAGES)

phase4c-contract:
	$(GO) test ./internal/provider/anthropic -run 'TestAnthropicProviderLocalContractTextAndCredentialRotation|TestOfficialOpenAISDKRoutesThroughAnthropicProvider' -count=20

phase4c-fault:
	$(GO) test ./internal/provider/anthropic -run 'Test(EventSourceRejects|EventSourceRedacts|ProviderClassifiesCredentialErrorsAndRedactsSecret|ProviderNormalizesStartupAndCancellation|NormalizeProviderError|BoundedRoundTripper)' -count=20

phase4c-stress:
	$(GO) test -race -tags=phase4cstress ./internal/provider/anthropic -run '^TestAnthropicProviderConcurrentStress$$' -count=3

phase4c-fuzz:
	$(GO) test ./internal/provider/anthropic -run='^$$' -fuzz='^FuzzAnthropicEventSourceRawEvent$$' -fuzztime=$(FUZZ_TIME)

phase4c-e2e:
	$(GO) test -tags=e2e ./test/e2e -run '^TestAnthropicProviderSubprocessAvailability$$' -count=3 -timeout=5m

phase4c-live-compile:
	$(GO) test -tags=liveanthropic ./internal/provider/anthropic -run '^$$' -count=1

phase4c-live:
	@test "$${ANT_ROUTER_LIVE_ANTHROPIC:-}" = "1" || { echo "ANT_ROUTER_LIVE_ANTHROPIC=1 is required" >&2; exit 2; }
	@test -n "$${ANTHROPIC_API_KEY:-}" || { echo "ANTHROPIC_API_KEY is required" >&2; exit 2; }
	@test -n "$${ANT_ROUTER_LIVE_ANTHROPIC_MODEL:-}" || { echo "ANT_ROUTER_LIVE_ANTHROPIC_MODEL is required" >&2; exit 2; }
	$(GO) test -tags=liveanthropic ./internal/provider/anthropic -run '^TestLiveAnthropicCertification$$' -count=1

phase4c-ci: phase4c-test phase4c-race phase4c-contract phase4c-fault phase4c-stress phase4c-e2e phase4c-live-compile

codex-alpha-test:
	$(GO) test $(CODEX_ALPHA_PACKAGES) -count=100

codex-alpha-race:
	$(GO) test -race $(CODEX_ALPHA_PACKAGES)

codex-alpha-e2e:
	$(GO) test -tags=e2e ./test/e2e -run '^TestCodexReplacementAlphaWorkflow$$' -count=3 -timeout=5m

codex-alpha-fuzz:
	$(GO) test ./internal/codexcatalog -run='^$$' -fuzz='^FuzzGenerate$$' -fuzztime=$(FUZZ_TIME)
	$(GO) test ./internal/codexintegration -run='^$$' -fuzz='^FuzzNewPlan$$' -fuzztime=$(FUZZ_TIME)
	$(GO) test ./internal/codexintegration -run='^$$' -fuzz='^FuzzDecodePending$$' -fuzztime=$(FUZZ_TIME)
	$(GO) test ./internal/codexownership -run='^$$' -fuzz='^FuzzDecodeStrict$$' -fuzztime=$(FUZZ_TIME)
	$(GO) test ./internal/codexprofile -run='^$$' -fuzz='^FuzzRender$$' -fuzztime=$(FUZZ_TIME)
	$(GO) test ./internal/codexversion -run='^$$' -fuzz='^FuzzParse$$' -fuzztime=$(FUZZ_TIME)
	$(GO) test ./internal/canonical -run='^$$' -fuzz='^FuzzToolNamespace$$' -fuzztime=$(FUZZ_TIME)

codex-alpha-ci: codex-alpha-test codex-alpha-race

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

# ci mirrors .github/workflows/ci.yml: the quality job steps plus the
# fuzz-smoke target (the workflow's fuzz job additionally runs per-package
# fuzz targets). The codex-cli-e2e job (needs the pinned Codex CLI) and the
# platform-tests macOS/Windows matrix stay workflow-only.
ci: verify config-test config-race fault-test control-test control-race control-fault configservice-test configservice-race cli-test cli-race phase1c-fault phase1c-e2e canonical-test canonical-race canonical-golden canonical-stress streaming-test streaming-race streaming-golden streaming-stress phase3a-ci phase3b-ci phase4a-ci phase4b-ci phase4c-ci codex-alpha-ci fuzz-smoke

.PHONY: release-check release-snapshot release-test

release-check:
	@which goreleaser > /dev/null || { echo "goreleaser not found; install from https://goreleaser.com/install/"; exit 1; }
	goreleaser check

release-snapshot:
	goreleaser release --snapshot --clean --skip=sign,publish

release-test:
	$(GO) test ./test/release/... -count=1

clean:
	rm -rf $(BIN_DIR)
