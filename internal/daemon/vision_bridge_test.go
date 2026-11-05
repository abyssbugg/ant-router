package daemon

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/abyssbugg/ant-router/internal/inferencetoken"
)

type upstreamCall struct {
	Model   string `json:"model"`
	Body    string
	Content string
}

func TestVisionBridgeRoutesImagesThroughVisionModel(t *testing.T) {
	var mu sync.Mutex
	var calls []upstreamCall
	chatServer := httptestVisionServer(t, &mu, &calls)
	defer chatServer.Close()

	stateRoot := t.TempDir()
	runtimeRoot := shortRuntimeRoot(t)
	listenConfig := net.ListenConfig{}
	dataListener, err := listenConfig.Listen(context.Background(), "tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() failed: %v", err)
	}
	t.Cleanup(func() { _ = dataListener.Close() })
	address := dataListener.Addr().String()

	configTOML := "schema_version = 8\n" +
		"[data_plane]\nenabled = true\nlisten = \"" + address + "\"\nmax_concurrent_requests = 4\nshutdown_timeout = \"2s\"\nvision_bridge = \"vision-model\"\n" +
		"[[providers]]\nid = \"vision\"\nkind = \"openai\"\nenabled = true\nbase_url = \"" + chatServer.URL + "\"\nwire = \"chat\"\n" +
		"[providers.credential]\nsource = \"env\"\nname = \"OPENAI_VISION_KEY\"\n[[providers.models]]\nalias = \"vision-model\"\nupstream = \"upstream-vision\"\ncapabilities = [\"text\", \"image_input\"]\n" +
		"[[providers]]\nid = \"text\"\nkind = \"openai\"\nenabled = true\nbase_url = \"" + chatServer.URL + "\"\nwire = \"chat\"\n" +
		"[providers.credential]\nsource = \"env\"\nname = \"OPENAI_TEXT_KEY\"\n[[providers.models]]\nalias = \"text-model\"\nupstream = \"upstream-text\"\ncapabilities = [\"text\"]\n"

	t.Setenv("OPENAI_VISION_KEY", "test-vision-key")
	t.Setenv("OPENAI_TEXT_KEY", "test-text-key")
	bootstrapOptions := daemonOptions(t, stateRoot, runtimeRoot)
	bootstrap, cancelBootstrap, bootstrapDone := startDaemon(t, bootstrapOptions)
	client := daemonClient(t, bootstrap)
	proof, err := client.ValidateConfig(context.Background(), []byte(configTOML))
	if err != nil {
		t.Fatalf("ValidateConfig() failed: %v", err)
	}
	if _, err := client.ApplyConfig(context.Background(), []byte(configTOML), proof); err != nil {
		t.Fatalf("ApplyConfig() failed: %v", err)
	}
	cancelBootstrap()
	if err := <-bootstrapDone; err != nil {
		t.Fatalf("bootstrap daemon Run() failed: %v", err)
	}

	options := daemonOptions(t, stateRoot, runtimeRoot)
	options.DataPlaneListen = func(requested string) (net.Listener, error) {
		if requested != address {
			t.Fatalf("data-plane listen request = %q", requested)
		}
		return dataListener, nil
	}
	d, cancel, done := startDaemon(t, options)
	defer func() { cancel(); <-done }()

	store, err := inferencetoken.NewStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	token, err := store.Load(d.paths.Instance)
	if err != nil {
		t.Fatal(err)
	}

	inference := `{"model":"text-model","input":[{"role":"user","content":[{"type":"input_text","text":"what is in the picture?"},{"type":"input_image","image_url":"https://example.com/pic.png"}]}]}`
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://"+address+"/v1/responses", strings.NewReader(inference))
	if err != nil {
		t.Fatal(err)
	}
	request.Host = address
	request.Header.Set("Authorization", "Bearer "+token.String())
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	body, _ := io.ReadAll(response.Body)
	mu.Lock()
	callsSeen := len(calls)
	bodies := append([]upstreamCall(nil), calls...)
	mu.Unlock()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("inference status = %d body = %s upstream calls = %d bodies = %v", response.StatusCode, body, callsSeen, bodies)
	}
	if len(calls) != 2 {
		t.Fatalf("upstream calls = %d, want 2 (describe then final); bodies: %v", len(calls), calls)
	}
	if bodies[0].Model != "upstream-vision" {
		t.Fatalf("first upstream model = %q, want upstream-vision", bodies[0].Model)
	}
	if !strings.Contains(bodies[0].Body, "https://example.com/pic.png") {
		t.Fatal("describe call lost the image")
	}
	if bodies[1].Model != "upstream-text" {
		t.Fatalf("second upstream model = %q, want upstream-text", bodies[1].Model)
	}
	if strings.Contains(bodies[1].Body, "https://example.com/pic.png") {
		t.Fatal("final call still carries the image URL")
	}
	if !strings.Contains(bodies[1].Body, "a red cube on a blue table") {
		t.Fatalf("final call missing the description text: %s", bodies[1].Body)
	}

}

func httptestVisionServer(t *testing.T, mu *sync.Mutex, calls *[]upstreamCall) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		var parsed struct {
			Model    string `json:"model"`
			Messages []struct {
				Content any `json:"content"`
			} `json:"messages"`
		}
		_ = json.Unmarshal(body, &parsed)
		mu.Lock()
		*calls = append(*calls, upstreamCall{Model: parsed.Model, Body: string(body)})
		mu.Unlock()
		description := "a red cube on a blue table"
		if parsed.Model == "upstream-text" {
			description = "the user asked about an image described earlier"
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		frames := []string{
			`{"id":"c1","model":"` + parsed.Model + `","choices":[{"index":0,"delta":{"role":"assistant"}}]}`,
			`{"id":"c1","choices":[{"index":0,"delta":{"content":"` + description + `"}}]}`,
			`{"id":"c1","choices":[{"index":0,"finish_reason":"stop","delta":{}}]}`,
			`{"id":"c1","choices":[],"usage":{"prompt_tokens":5,"completion_tokens":5,"total_tokens":10}}`,
			`[DONE]`,
		}
		for _, frame := range frames {
			_, _ = io.WriteString(writer, "data: "+frame+"\n\n")
		}
		if flusher, ok := writer.(http.Flusher); ok {
			flusher.Flush()
		}
	}))
	t.Cleanup(server.Close)
	return server
}
