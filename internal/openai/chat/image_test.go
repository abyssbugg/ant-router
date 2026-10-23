package chat

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

func TestEncodeMessageWithURLImageIncludesImageURLPart(t *testing.T) {
	text, err := canonical.NewTextContent("describe this")
	if err != nil {
		t.Fatal(err)
	}
	textBlock, err := canonical.NewTextBlock(text)
	if err != nil {
		t.Fatal(err)
	}

	image, err := canonical.NewRemoteImage("https://example.com/image.png", "image/png", canonical.ImageDetail("high"), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	imageBlock, err := canonical.NewImageBlock(image)
	if err != nil {
		t.Fatal(err)
	}

	userMessage, err := canonical.NewMessage(fixedItemID(t, 1), canonical.Role("user"), []canonical.ContentBlock{textBlock, imageBlock})
	if err != nil {
		t.Fatal(err)
	}
	userItem, err := canonical.NewMessageItem(userMessage)
	if err != nil {
		t.Fatal(err)
	}

	request, err := canonical.NewRequest(canonical.RequestParams{
		ID:    fixedRequestID(t, 1),
		Model: "gpt-4-vision",
		Input: []canonical.Item{userItem},
	})
	if err != nil {
		t.Fatal(err)
	}

	body, err := EncodeRequest(request, "gpt-4-vision", false)
	if err != nil {
		t.Fatal(err)
	}

	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		t.Fatal(err)
	}

	messages := data["messages"].([]any)
	if len(messages) == 0 {
		t.Fatal("no messages")
	}

	msg := messages[0].(map[string]any)
	content, ok := msg["content"].([]any)
	if !ok {
		t.Fatalf("content is not array: %T", msg["content"])
	}

	if len(content) != 2 {
		t.Fatalf("expected 2 content parts (text + image), got %d", len(content))
	}

	// Check text part
	textPart := content[0].(map[string]any)
	if textPart["type"] != "text" {
		t.Fatalf("first part type is not 'text': %v", textPart["type"])
	}
	if textPart["text"] != "describe this" {
		t.Fatalf("text content mismatch: %v", textPart["text"])
	}

	// Check image part
	imagePart := content[1].(map[string]any)
	if imagePart["type"] != "image_url" {
		t.Fatalf("second part type is not 'image_url': %v", imagePart["type"])
	}

	imageURL := imagePart["image_url"].(map[string]any)
	if imageURL["url"] != "https://example.com/image.png" {
		t.Fatalf("image URL mismatch: %v", imageURL["url"])
	}
	if imageURL["detail"] != "high" {
		t.Fatalf("image detail mismatch: %v", imageURL["detail"])
	}
}

func TestEncodeMessageWithInlineImageUsesDataURL(t *testing.T) {
	imageData := []byte{0x89, 0x50, 0x4E, 0x47} // PNG magic bytes
	image, err := canonical.NewInlineImage(imageData, "image/png", canonical.ImageDetail("low"), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	imageBlock, err := canonical.NewImageBlock(image)
	if err != nil {
		t.Fatal(err)
	}

	userMessage, err := canonical.NewMessage(fixedItemID(t, 1), canonical.Role("user"), []canonical.ContentBlock{imageBlock})
	if err != nil {
		t.Fatal(err)
	}
	userItem, err := canonical.NewMessageItem(userMessage)
	if err != nil {
		t.Fatal(err)
	}

	request, err := canonical.NewRequest(canonical.RequestParams{
		ID:    fixedRequestID(t, 1),
		Model: "gpt-4-vision",
		Input: []canonical.Item{userItem},
	})
	if err != nil {
		t.Fatal(err)
	}

	body, err := EncodeRequest(request, "gpt-4-vision", false)
	if err != nil {
		t.Fatal(err)
	}

	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		t.Fatal(err)
	}

	messages := data["messages"].([]any)
	msg := messages[0].(map[string]any)
	content := msg["content"].([]any)
	imagePart := content[0].(map[string]any)

	imageURL := imagePart["image_url"].(map[string]any)
	url := imageURL["url"].(string)

	if !strings.HasPrefix(url, "data:image/png;base64,") {
		t.Fatalf("URL does not use data: scheme: %v", url)
	}

	// Verify base64 encoding of the PNG magic bytes
	if !strings.Contains(url, "iVBO") { // base64 for 0x89, 0x50, 0x4E, 0x47
		t.Fatalf("base64 encoding incorrect: %v", url)
	}
}

func TestEncodeMessageWithMixedTextAndImagePreservesOrder(t *testing.T) {
	text1, err := canonical.NewTextContent("first")
	if err != nil {
		t.Fatal(err)
	}
	text1Block, err := canonical.NewTextBlock(text1)
	if err != nil {
		t.Fatal(err)
	}

	image, err := canonical.NewRemoteImage("https://example.com/img.jpg", "image/jpeg", canonical.ImageDetailLow, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	imageBlock, err := canonical.NewImageBlock(image)
	if err != nil {
		t.Fatal(err)
	}

	text2, err := canonical.NewTextContent("last")
	if err != nil {
		t.Fatal(err)
	}
	text2Block, err := canonical.NewTextBlock(text2)
	if err != nil {
		t.Fatal(err)
	}

	userMessage, err := canonical.NewMessage(fixedItemID(t, 1), canonical.Role("user"), []canonical.ContentBlock{text1Block, imageBlock, text2Block})
	if err != nil {
		t.Fatal(err)
	}
	userItem, err := canonical.NewMessageItem(userMessage)
	if err != nil {
		t.Fatal(err)
	}

	request, err := canonical.NewRequest(canonical.RequestParams{
		ID:    fixedRequestID(t, 1),
		Model: "gpt-4-vision",
		Input: []canonical.Item{userItem},
	})
	if err != nil {
		t.Fatal(err)
	}

	body, err := EncodeRequest(request, "gpt-4-vision", false)
	if err != nil {
		t.Fatal(err)
	}

	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		t.Fatal(err)
	}

	messages := data["messages"].([]any)
	msg := messages[0].(map[string]any)
	content := msg["content"].([]any)

	if len(content) != 3 {
		t.Fatalf("expected 3 content parts, got %d", len(content))
	}

	// Verify order: text, image, text
	if content[0].(map[string]any)["type"] != "text" || content[0].(map[string]any)["text"] != "first" {
		t.Fatal("first part should be text 'first'")
	}
	if content[1].(map[string]any)["type"] != "image_url" {
		t.Fatal("second part should be image_url")
	}
	if content[2].(map[string]any)["type"] != "text" || content[2].(map[string]any)["text"] != "last" {
		t.Fatal("third part should be text 'last'")
	}
}

func TestEncodeMessageWithoutImageStaysPlainString(t *testing.T) {
	text, err := canonical.NewTextContent("plain message")
	if err != nil {
		t.Fatal(err)
	}
	textBlock, err := canonical.NewTextBlock(text)
	if err != nil {
		t.Fatal(err)
	}

	userMessage, err := canonical.NewMessage(fixedItemID(t, 1), canonical.Role("user"), []canonical.ContentBlock{textBlock})
	if err != nil {
		t.Fatal(err)
	}
	userItem, err := canonical.NewMessageItem(userMessage)
	if err != nil {
		t.Fatal(err)
	}

	request, err := canonical.NewRequest(canonical.RequestParams{
		ID:    fixedRequestID(t, 1),
		Model: "gpt-4",
		Input: []canonical.Item{userItem},
	})
	if err != nil {
		t.Fatal(err)
	}

	body, err := EncodeRequest(request, "gpt-4", false)
	if err != nil {
		t.Fatal(err)
	}

	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		t.Fatal(err)
	}

	messages := data["messages"].([]any)
	msg := messages[0].(map[string]any)
	content := msg["content"]

	// Should be string, not array
	contentStr, ok := content.(string)
	if !ok {
		t.Fatalf("content should be string for text-only message, got %T", content)
	}
	if contentStr != "plain message" {
		t.Fatalf("content mismatch: %v", contentStr)
	}
}

func TestEncodeMessageRejectsImageInSystemMessage(t *testing.T) {
	image, err := canonical.NewRemoteImage("https://example.com/image.png", "image/png", canonical.ImageDetailAuto, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	imageBlock, err := canonical.NewImageBlock(image)
	if err != nil {
		t.Fatal(err)
	}

	systemMessage, err := canonical.NewMessage(fixedItemID(t, 1), canonical.Role("system"), []canonical.ContentBlock{imageBlock})
	if err != nil {
		t.Fatal(err)
	}
	systemItem, err := canonical.NewMessageItem(systemMessage)
	if err != nil {
		t.Fatal(err)
	}

	request, err := canonical.NewRequest(canonical.RequestParams{
		ID:    fixedRequestID(t, 1),
		Model: "gpt-4",
		Input: []canonical.Item{systemItem},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = EncodeRequest(request, "gpt-4", false)
	if err == nil {
		t.Fatal("expected error for image in system message")
	}
}

func TestEncodeMessageRejectsImageInAssistantMessage(t *testing.T) {
	image, err := canonical.NewRemoteImage("https://example.com/image.png", "image/png", canonical.ImageDetailAuto, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	imageBlock, err := canonical.NewImageBlock(image)
	if err != nil {
		t.Fatal(err)
	}

	assistantMessage, err := canonical.NewMessage(fixedItemID(t, 1), canonical.Role("assistant"), []canonical.ContentBlock{imageBlock})
	if err != nil {
		t.Fatal(err)
	}
	assistantItem, err := canonical.NewMessageItem(assistantMessage)
	if err != nil {
		t.Fatal(err)
	}

	request, err := canonical.NewRequest(canonical.RequestParams{
		ID:    fixedRequestID(t, 1),
		Model: "gpt-4",
		Input: []canonical.Item{assistantItem},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = EncodeRequest(request, "gpt-4", false)
	if err == nil {
		t.Fatal("expected error for image in assistant message")
	}
}

func TestEncodeImageWithoutDetailOmitsDetailField(t *testing.T) {
	image, err := canonical.NewRemoteImage("https://example.com/image.png", "image/png", canonical.ImageDetailAuto, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	imageBlock, err := canonical.NewImageBlock(image)
	if err != nil {
		t.Fatal(err)
	}

	userMessage, err := canonical.NewMessage(fixedItemID(t, 1), canonical.Role("user"), []canonical.ContentBlock{imageBlock})
	if err != nil {
		t.Fatal(err)
	}
	userItem, err := canonical.NewMessageItem(userMessage)
	if err != nil {
		t.Fatal(err)
	}

	request, err := canonical.NewRequest(canonical.RequestParams{
		ID:    fixedRequestID(t, 1),
		Model: "gpt-4-vision",
		Input: []canonical.Item{userItem},
	})
	if err != nil {
		t.Fatal(err)
	}

	body, err := EncodeRequest(request, "gpt-4-vision", false)
	if err != nil {
		t.Fatal(err)
	}

	encoded := string(body)
	// Should not have a "detail" field when unset
	if strings.Contains(encoded, `"detail":`) {
		t.Fatal("detail field should not be present when unset")
	}
}

// Chat Completions tool messages carry text only, so an image in a tool
// result must be rejected rather than dropped.
func TestEncodeMessagesRejectsImageInToolResult(t *testing.T) {
	ids := &countingIDs{next: 100}
	callID, err := ids.ToolCallID()
	if err != nil {
		t.Fatal(err)
	}
	call, err := canonical.NewToolCall(fixedItemID(t, 10), callID, "lookup", mustSchema(t))
	if err != nil {
		t.Fatal(err)
	}
	callItem, err := canonical.NewToolCallItem(call)
	if err != nil {
		t.Fatal(err)
	}
	image, err := canonical.NewRemoteImage("https://example.com/result.png", "image/png", canonical.ImageDetailAuto, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	imageBlock, err := canonical.NewImageBlock(image)
	if err != nil {
		t.Fatal(err)
	}
	result, err := canonical.NewToolResult(fixedItemID(t, 20), callID, []canonical.ContentBlock{imageBlock}, false)
	if err != nil {
		t.Fatal(err)
	}
	resultItem, err := canonical.NewToolResultItem(result)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := encodeMessages([]canonical.Item{callItem, resultItem}); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("err = %v, want unsupported for an image tool result", err)
	}
}

// Adjacent text blocks read the same with or without an image: they are joined
// with a newline into one text part, exactly as the text-only path joins them.
func TestEncodeMessageWithImageJoinsAdjacentTextBlocks(t *testing.T) {
	textBlock := func(value string) canonical.ContentBlock {
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
	image, err := canonical.NewRemoteImage("https://example.com/img.jpg", "image/jpeg", canonical.ImageDetailLow, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	imageBlock, err := canonical.NewImageBlock(image)
	if err != nil {
		t.Fatal(err)
	}
	message, err := canonical.NewMessage(fixedItemID(t, 1), canonical.Role("user"), []canonical.ContentBlock{textBlock("one"), textBlock("two"), imageBlock, textBlock("three")})
	if err != nil {
		t.Fatal(err)
	}
	item, err := canonical.NewMessageItem(message)
	if err != nil {
		t.Fatal(err)
	}
	request, err := canonical.NewRequest(canonical.RequestParams{ID: fixedRequestID(t, 1), Model: "gpt-4-vision", Input: []canonical.Item{item}})
	if err != nil {
		t.Fatal(err)
	}
	body, err := EncodeRequest(request, "gpt-4-vision", false)
	if err != nil {
		t.Fatal(err)
	}
	var data struct {
		Messages []struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, part := range data.Messages[0].Content {
		got = append(got, part.Type+":"+part.Text)
	}
	want := []string{"text:one\ntwo", "image_url:", "text:three"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("content parts = %q, want %q", got, want)
	}
}
