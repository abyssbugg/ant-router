package configservice

import (
	"context"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/config"
	"github.com/abyssbugg/ant-router/internal/configstore"
)

func FuzzValidateConfiguration(f *testing.F) {
	store, err := configstore.NewFileStore(f.TempDir())
	if err != nil {
		f.Fatalf("NewFileStore() failed: %v", err)
	}
	manager, err := config.NewManager(store, time.Now)
	if err != nil {
		f.Fatalf("NewManager() failed: %v", err)
	}
	service, err := New(manager, store)
	if err != nil {
		f.Fatalf("New() failed: %v", err)
	}
	if _, err := service.Initialize(context.Background()); err != nil {
		f.Fatalf("Initialize() failed: %v", err)
	}
	f.Add([]byte("schema_version = 1\n"))
	f.Add([]byte("schema_version = 1\n[daemon]\nshutdown_timeout = \"1s\"\n"))
	f.Add([]byte("# comment\nschema_version = 1\n[daemon]\nshutdown_timeout = \"10m\"\n"))
	f.Add([]byte("not TOML"))
	f.Fuzz(func(t *testing.T, data []byte) {
		result, err := service.Validate(context.Background(), data)
		if err != nil {
			return
		}
		if !result.Revision.Valid() || result.CanonicalSize <= 0 || result.BaseGeneration != 0 {
			t.Fatalf("Validate accepted inconsistent result: %#v", result)
		}
	})
}
