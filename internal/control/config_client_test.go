package control

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/config"
	"github.com/abyssbugg/ant-router/internal/controltoken"
)

func TestConfigClientWorkflow(t *testing.T) {
	revision := strings.Repeat("a", 64)
	var validated, applied string
	activatedAt := time.Date(2026, 8, 19, 18, 0, 0, 0, time.UTC)
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/config":
			_ = WriteJSON(writer, http.StatusOK, ConfigCurrentResponse{State: "running", Revision: revision, Generation: 5, Status: "active", ActivatedAt: &activatedAt})
		case "/v1/config/validate":
			var body ConfigValidateRequest
			_ = json.NewDecoder(request.Body).Decode(&body)
			validated = body.TOML
			_ = WriteJSON(writer, http.StatusOK, ConfigValidateResponse{Revision: revision, BaseGeneration: 4, CanonicalSize: 142})
		case "/v1/config/apply":
			var body ConfigApplyRequest
			_ = json.NewDecoder(request.Body).Decode(&body)
			applied = body.TOML
			if body.ExpectedRevision != revision || body.ExpectedGeneration != 4 {
				t.Errorf("apply proof = %#v", body)
			}
			_ = WriteJSON(writer, http.StatusOK, ConfigMutationResponse{State: "running", Revision: revision, Generation: 5, Status: "active", ActivatedAt: time.Now().UTC()})
		case "/v1/config/rollback":
			_ = WriteJSON(writer, http.StatusOK, ConfigMutationResponse{State: "running", Revision: strings.Repeat("b", 64), Generation: 6, Status: "active", ActivatedAt: time.Now().UTC()})
		default:
			_ = WriteError(writer, http.StatusNotFound, CodeNotFound, "request-123")
		}
	})
	client, _, _ := testClient(t, handler)
	current, err := client.CurrentConfig(context.Background())
	if err != nil || current.Generation != 5 {
		t.Fatalf("CurrentConfig() = %#v, %v", current, err)
	}
	tomlBytes := []byte("schema_version = 1\n")
	proof, err := client.ValidateConfig(context.Background(), tomlBytes)
	if err != nil || proof.Revision != revision || validated != string(tomlBytes) {
		t.Fatalf("ValidateConfig() = %#v, %v, TOML %q", proof, err, validated)
	}
	activated, err := client.ApplyConfig(context.Background(), tomlBytes, proof)
	if err != nil || activated.Generation != 5 || applied != string(tomlBytes) {
		t.Fatalf("ApplyConfig() = %#v, %v, TOML %q", activated, err, applied)
	}
	rolled, err := client.RollbackConfig(context.Background())
	if err != nil || rolled.Generation != 6 {
		t.Fatalf("RollbackConfig() = %#v, %v", rolled, err)
	}
}

func TestConfigClientRejectsInvalidUTF8BeforeTokenOrDial(t *testing.T) {
	tokenCalls, dials := 0, 0
	client, err := NewClient(clientDialerFunc(func(context.Context) (net.Conn, error) {
		dials++
		return nil, context.Canceled
	}), tokenSourceFunc(func(context.Context) (controltoken.Token, error) {
		tokenCalls++
		return controltoken.Token{}, nil
	}), DefaultClientOptions())
	if err != nil {
		t.Fatalf("NewClient() failed: %v", err)
	}
	invalid := []byte{0xff, 0xfe}
	if _, err := client.ValidateConfig(context.Background(), invalid); ClientErrorKindValue(err) != ClientErrorProtocol {
		t.Fatalf("ValidateConfig kind = %q, want %q", ClientErrorKindValue(err), ClientErrorProtocol)
	}
	proof := ConfigValidateResponse{Revision: strings.Repeat("a", 64), BaseGeneration: 0, CanonicalSize: 2}
	if _, err := client.ApplyConfig(context.Background(), invalid, proof); ClientErrorKindValue(err) != ClientErrorProtocol {
		t.Fatalf("ApplyConfig kind = %q, want %q", ClientErrorKindValue(err), ClientErrorProtocol)
	}
	if tokenCalls != 0 || dials != 0 {
		t.Errorf("invalid UTF-8 reached token/dial: token=%d dial=%d", tokenCalls, dials)
	}
}

func TestConfigClientRejectsOversizedTOMLBeforeDial(t *testing.T) {
	dials := 0
	client, err := NewClient(clientDialerFunc(func(context.Context) (net.Conn, error) {
		dials++
		return nil, context.Canceled
	}), tokenSourceFunc(func(context.Context) (controltoken.Token, error) {
		t.Fatal("token source was called")
		return controltoken.Token{}, nil
	}), DefaultClientOptions())
	if err != nil {
		t.Fatalf("NewClient() failed: %v", err)
	}
	_, err = client.ValidateConfig(context.Background(), bytes.Repeat([]byte{'x'}, config.MaxSourceBytes+1))
	if ClientErrorKindValue(err) != ClientErrorProtocol || dials != 0 {
		t.Fatalf("oversized result kind/dials = %q/%d", ClientErrorKindValue(err), dials)
	}
}
