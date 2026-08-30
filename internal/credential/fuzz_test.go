package credential

import "testing"

func FuzzEnvironmentReference(f *testing.F) {
	for _, seed := range []string{"OPENAI_API_KEY", "_KEY", "A", "", "openai_key", "OPENAI-KEY", "../KEY"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, name string) {
		ref, err := NewRef(SourceEnv, name)
		valid := len(name) > 0 && len(name) <= MaxNameBytes && environmentNamePattern.MatchString(name)
		if valid {
			if err != nil || !ref.Valid() || ref.Name() != name {
				t.Fatalf("valid name %q rejected: %v", name, err)
			}
			return
		}
		if err == nil {
			t.Fatalf("invalid name %q accepted", name)
		}
	})
}
