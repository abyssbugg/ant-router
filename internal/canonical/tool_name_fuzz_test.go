package canonical

import "testing"

func FuzzToolName(f *testing.F) {
	f.Add("search")
	f.Add("Search_1")
	f.Add("bad name")
	f.Fuzz(func(t *testing.T, value string) {
		if !validToolName(value) {
			return
		}
		if len(value) < 1 || len(value) > MaxToolNameBytes {
			t.Fatalf("accepted invalid tool name length")
		}
		if !((value[0] >= 'a' && value[0] <= 'z') || (value[0] >= 'A' && value[0] <= 'Z')) {
			t.Fatalf("accepted non-letter tool prefix %q", value[0])
		}
	})
}
