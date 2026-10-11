package mock

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/execution"
	"github.com/abyssbugg/ant-router/internal/streaming"
)

func TestCatalogIncludesVisionCapability(t *testing.T) {
	provider, err := New(Options{IDs: &sequenceIDs{}})
	if err != nil {
		t.Fatal(err)
	}
	models, err := provider.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"mock/codex", "mock/disconnect", "mock/json", "mock/reasoning", "mock/slow", "mock/text", "mock/tool", "mock/vision"}
	if len(models) != len(want) {
		t.Fatalf("model count=%d want=%d", len(models), len(want))
	}
	for index, id := range want {
		if models[index].ID() != id {
			t.Fatalf("model[%d]=%q want=%q", index, models[index].ID(), id)
		}
	}
	caps := models[len(models)-1].Capabilities()
	if !caps.Has(canonical.CapabilityText) || !caps.Has(canonical.CapabilityImageInput) {
		t.Fatalf("vision capabilities=%v", caps.Values())
	}
}

func TestVisionScenarioProducesSafeDeterministicSummary(t *testing.T) {
	inlineBytes := []byte("private-inline-image-bytes")
	request := visionRequest(t,
		textBlock(t, "inspect"),
		remoteImageBlock(t, "https://secret.example/private/path?token=private", canonical.ImageDetailHigh),
		inlineImageBlock(t, inlineBytes, "image/png", canonical.ImageDetailLow, 12, 7),
	)
	provider, err := New(Options{IDs: &sequenceIDs{}})
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := sha256.Sum256(inlineBytes)
	want := "images=2\n" +
		"image[0]: source=remote mime=unknown detail=high dimensions=unknown\n" +
		"image[1]: source=inline mime=image/png detail=low dimensions=12x7 sha256=" + hex.EncodeToString(wantDigest[:])[:visionDigestHexLength]
	for attempt := 0; attempt < 2; attempt++ {
		source, executeErr := provider.Execute(context.Background(), request)
		if executeErr != nil {
			t.Fatal(executeErr)
		}
		response := reduceSource(t, source)
		message, ok := response.Items()[0].Message()
		if !ok || len(message.Content()) != 1 {
			t.Fatalf("vision response item=%#v", response.Items()[0])
		}
		text, ok := message.Content()[0].Text()
		if !ok || text.Text() != want {
			t.Fatalf("vision summary=%q want=%q", text.Text(), want)
		}
		for _, secret := range []string{"secret.example", "/private/path", "token=private", string(inlineBytes), hex.EncodeToString(wantDigest[:])} {
			if strings.Contains(text.Text(), secret) {
				t.Fatalf("vision summary leaked private input: %q", text.Text())
			}
		}
	}
}

func TestVisionScenarioRequiresImage(t *testing.T) {
	provider, _ := New(Options{IDs: &sequenceIDs{}})
	_, err := provider.Execute(context.Background(), scenarioRequest(t, "mock/vision"))
	if execution.CodeOf(err) != execution.CodeInvalidRequest {
		t.Fatalf("Execute() error=%v", err)
	}
}

func TestExecuteRejectsInvalidRequest(t *testing.T) {
	provider, _ := New(Options{IDs: &sequenceIDs{}})
	_, err := provider.Execute(context.Background(), canonical.Request{})
	if execution.CodeOf(err) != execution.CodeInvalidRequest {
		t.Fatalf("Execute() error=%v", err)
	}
}

func TestTextModelRejectsImageCapability(t *testing.T) {
	provider, _ := New(Options{IDs: &sequenceIDs{}})
	request := visionRequest(t, remoteImageBlock(t, "https://example.com/image", canonical.ImageDetailAuto))
	request = cloneRequestModel(t, request, "mock/text")
	_, err := provider.Execute(context.Background(), request)
	if execution.CodeOf(err) != execution.CodeInvalidModel {
		t.Fatalf("Execute() error=%v", err)
	}
}

func TestVisionScenarioIsDeterministicConcurrently(t *testing.T) {
	provider, _ := New(Options{IDs: &sequenceIDs{}})
	request := visionRequest(t, inlineImageBlock(t, []byte("image"), "image/jpeg", canonical.ImageDetailAuto, 2, 3))
	const workers = 32
	type executionResult struct {
		source streaming.EventSource
		err    error
	}
	results := make(chan executionResult, workers)
	var group sync.WaitGroup
	for index := 0; index < workers; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			source, err := provider.Execute(context.Background(), request)
			results <- executionResult{source: source, err: err}
		}()
	}
	group.Wait()
	close(results)
	var first string
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent Execute() failed: %v", result.err)
		}
		response := reduceSource(t, result.source)
		message, ok := response.Items()[0].Message()
		if !ok || len(message.Content()) != 1 {
			t.Fatalf("concurrent response item=%#v", response.Items()[0])
		}
		text, ok := message.Content()[0].Text()
		if !ok {
			t.Fatal("concurrent response content is not text")
		}
		if first == "" {
			first = text.Text()
		}
		if text.Text() != first {
			t.Fatalf("nondeterministic result=%q first=%q", text.Text(), first)
		}
	}
}

func visionRequest(t *testing.T, blocks ...canonical.ContentBlock) canonical.Request {
	t.Helper()
	ids := &sequenceIDs{}
	itemID, _ := ids.ItemID()
	message, err := canonical.NewMessage(itemID, canonical.Role("user"), blocks)
	if err != nil {
		t.Fatal(err)
	}
	item, _ := canonical.NewMessageItem(message)
	choice, _ := canonical.NewToolChoice(canonical.ToolChoiceKind("auto"), "")
	output, _ := canonical.NewOutputConstraints(canonical.OutputParams{Format: canonical.OutputFormat("text")})
	sampling, _ := canonical.NewSampling(canonical.SamplingParams{})
	requestID, _ := ids.RequestID()
	request, err := canonical.NewRequest(canonical.RequestParams{ID: requestID, Model: "mock/vision", Input: []canonical.Item{item}, ToolChoice: choice, Output: output, Sampling: sampling})
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func textBlock(t *testing.T, value string) canonical.ContentBlock {
	t.Helper()
	text, err := canonical.NewTextContent(value)
	if err != nil {
		t.Fatal(err)
	}
	block, err := canonical.NewTextBlock(text)
	if err != nil {
		t.Fatal(err)
	}
	return block
}

func remoteImageBlock(t *testing.T, url string, detail canonical.ImageDetail) canonical.ContentBlock {
	t.Helper()
	image, err := canonical.NewRemoteImage(url, "", detail, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	block, err := canonical.NewImageBlock(image)
	if err != nil {
		t.Fatal(err)
	}
	return block
}

func inlineImageBlock(t *testing.T, data []byte, mime string, detail canonical.ImageDetail, width, height int) canonical.ContentBlock {
	t.Helper()
	image, err := canonical.NewInlineImage(data, mime, detail, width, height)
	if err != nil {
		t.Fatal(err)
	}
	block, err := canonical.NewImageBlock(image)
	if err != nil {
		t.Fatal(err)
	}
	return block
}
