package codexversion

import "testing"

func TestParseCodexVersion(t *testing.T) {
	version, err := Parse("codex-cli 0.153.4\n")
	if err != nil {
		t.Fatal(err)
	}
	if version.Major != 0 || version.Minor != 153 || version.Patch != 4 || version.String() != "0.153.4" {
		t.Fatalf("version = %#v", version)
	}
}

func TestCheckSupportsTestedMinorLine(t *testing.T) {
	for _, output := range []string{"codex-cli 0.153.0", "codex-cli 0.153.4", "codex-cli 0.153.999"} {
		if _, err := Check(output, false); err != nil {
			t.Fatalf("%q: %v", output, err)
		}
	}
}

func TestCheckRejectsUnsupportedVersions(t *testing.T) {
	for _, output := range []string{"codex-cli 0.152.9", "codex-cli 0.154.0", "codex-cli 1.0.0", "codex 0.153.4", "secret"} {
		if _, err := Check(output, false); err == nil {
			t.Fatalf("accepted %q", output)
		}
	}
}

func TestCheckAllowsOnlyNewerUntestedOverride(t *testing.T) {
	if _, err := Check("codex-cli 0.154.0", true); err != nil {
		t.Fatal(err)
	}
	if _, err := Check("codex-cli 0.152.9", true); err == nil {
		t.Fatal("override accepted obsolete version")
	}
}
