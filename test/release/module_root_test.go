package release

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindModuleRoot(t *testing.T) {
	cases := []struct {
		name   string
		goMod  string
		wantOK bool
	}{
		{"lf", "module " + modulePath + "\n\ngo 1.25\n", true},
		{"crlf", "module " + modulePath + "\r\n\r\ngo 1.25\r\n", true},
		{"no trailing newline", "module " + modulePath, true},
		{"other module", "module example.com/other\n", false},
		{"prefix of path", "module " + modulePath + "-fork\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(tc.goMod), 0o600); err != nil {
				t.Fatal(err)
			}
			start := filepath.Join(root, "test", "release")
			if err := os.MkdirAll(start, 0o700); err != nil {
				t.Fatal(err)
			}
			got, visited, ok := findModuleRoot(start, modulePath)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (visited %v)", ok, tc.wantOK, visited)
			}
			if ok && got != filepath.Clean(root) {
				t.Fatalf("root = %q, want %q", got, root)
			}
			if len(visited) < 3 {
				t.Fatalf("visited = %v, want the start dir and its ancestors", visited)
			}
		})
	}
}

func TestReadTextNormalizesCRLF(t *testing.T) {
	path := filepath.Join(t.TempDir(), "go.mod")
	if err := os.WriteFile(path, []byte("module "+modulePath+"\r\n\r\ntoolchain go1.27.2\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := readText(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := "module " + modulePath + "\n\ntoolchain go1.27.2\n"; string(data) != want {
		t.Fatalf("readText = %q, want %q", data, want)
	}
}
