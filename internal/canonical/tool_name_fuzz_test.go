package canonical

import "testing"

func FuzzToolName(f *testing.F) {
	f.Add("search")
	f.Add("Search_1")
	f.Add("bad name")
	f.Add("search@1")
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
		for index := 1; index < len(value); index++ {
			character := value[index]
			if !((character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || character == '_' || character == '-') {
				t.Fatalf("accepted invalid tool character %q", character)
			}
		}
	})
}
