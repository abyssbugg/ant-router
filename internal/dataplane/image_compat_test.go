package dataplane_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	openairesponses "github.com/openai/openai-go/v3/responses"
)

func TestOfficialOpenAISDKImageResponses(t *testing.T) {
	baseURL, token, stop := startSDKServer(t)
	defer stop()
	client := openai.NewClient(option.WithAPIKey(token.String()), option.WithBaseURL(baseURL))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	png := sdkPNGHeader(12, 7)
	digest := sha256.Sum256(png)
	for _, detail := range []string{"low", "high"} {
		params := sdkVisionParams(dataImageURL("image/png", png), detail)
		response, err := client.Responses.New(ctx, params)
		if err != nil {
			t.Fatalf("Responses.New(%s) failed: %v", detail, err)
		}
		if response == nil {
			t.Fatalf("Responses.New(%s) returned nil", detail)
		}
		want := "images=1\nimage[0]: source=inline mime=image/png detail=" + detail + " dimensions=12x7 sha256=" + hex.EncodeToString(digest[:])[:12]
		if response.Model != "mock/vision" || response.OutputText() != want {
			t.Fatalf("response(%s) model=%q text=%q", detail, response.Model, response.OutputText())
		}
	}

	params := sdkVisionParams("https://secret.example/private/image?token=private", "auto")
	stream := client.Responses.NewStreaming(ctx, params)
	var delta strings.Builder
	seenCreated := false
	seenCompleted := false
	for stream.Next() {
		event := stream.Current()
		switch event.Type {
		case "response.created":
			seenCreated = true
		case "response.output_text.delta":
			delta.WriteString(event.Delta)
		case "response.completed":
			seenCompleted = true
		}
	}
	streamErr := stream.Err()
	closeErr := stream.Close()
	if streamErr != nil {
		t.Fatalf("Responses.NewStreaming() failed: %v", streamErr)
	}
	if closeErr != nil {
		t.Fatalf("stream Close() failed: %v", closeErr)
	}
	want := "images=1\nimage[0]: source=remote mime=unknown detail=auto dimensions=unknown"
	if !seenCreated || !seenCompleted || delta.String() != want {
		t.Fatalf("stream created=%t completed=%t delta=%q", seenCreated, seenCompleted, delta.String())
	}
	for _, secret := range []string{"secret.example", "/private/image", "token=private"} {
		if strings.Contains(delta.String(), secret) {
			t.Fatalf("stream leaked remote image input: %q", delta.String())
		}
	}
}

func sdkVisionParams(source, detail string) openairesponses.ResponseNewParams {
	image := openairesponses.ResponseInputContentParamOfInputImage(openairesponses.ResponseInputImageDetail(detail))
	image.OfInputImage.ImageURL = param.NewOpt(source)
	message := openairesponses.ResponseInputItemParamOfMessage(
		openairesponses.ResponseInputMessageContentListParam{
			openairesponses.ResponseInputContentParamOfInputText("inspect"),
			image,
		},
		openairesponses.EasyInputMessageRole("user"),
	)
	return openairesponses.ResponseNewParams{
		Model: "mock/vision",
		Input: openairesponses.ResponseNewParamsInputUnion{
			OfInputItemList: openairesponses.ResponseInputParam{message},
		},
	}
}

func dataImageURL(mime string, data []byte) string {
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}

func sdkPNGHeader(width, height int) []byte {
	data := make([]byte, 24)
	copy(data, []byte("\x89PNG\r\n\x1a\n"))
	binary.BigEndian.PutUint32(data[8:12], 13)
	copy(data[12:16], "IHDR")
	binary.BigEndian.PutUint32(data[16:20], uint32(width))
	binary.BigEndian.PutUint32(data[20:24], uint32(height))
	return data
}
