package install

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// requireToolsEnv lists (comma-separated, or "all") the external tools CI
// guarantees. A missing listed tool fails the test instead of skipping it,
// because a skipped installer test is not evidence that the path works.
const requireToolsEnv = "ANT_ROUTER_REQUIRE_INSTALL_TOOLS"

func toolRequired(name string) bool {
	for _, v := range strings.Split(os.Getenv(requireToolsEnv), ",") {
		v = strings.TrimSpace(v)
		if v == "all" || v == name {
			return true
		}
	}
	return false
}

// lookTool returns the path of name, skipping the test when it is missing
// unless toolRequired(name), in which case the test fails.
func lookTool(t *testing.T, name string) string {
	t.Helper()
	p, err := exec.LookPath(name)
	if err == nil {
		return p
	}
	if toolRequired(name) {
		t.Fatalf("%s is required by %s but not installed: %v", name, requireToolsEnv, err)
	}
	t.Skipf("%s not installed", name)
	return ""
}

func TestToolRequiredParsesList(t *testing.T) {
	t.Setenv(requireToolsEnv, "pwsh, wget")
	for name, want := range map[string]bool{"pwsh": true, "wget": true, "shellcheck": false} {
		if got := toolRequired(name); got != want {
			t.Fatalf("toolRequired(%q) = %v, want %v", name, got, want)
		}
	}
	t.Setenv(requireToolsEnv, "all")
	if !toolRequired("anything") {
		t.Fatal(`"all" must require every tool`)
	}
	t.Setenv(requireToolsEnv, "")
	if toolRequired("pwsh") {
		t.Fatal("empty list must not require tools")
	}
}
