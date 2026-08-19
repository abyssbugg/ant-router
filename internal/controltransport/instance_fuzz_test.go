package controltransport

import "testing"

func FuzzParseInstanceName(f *testing.F) {
	f.Add("default")
	f.Add("test-1")
	f.Add("../escape")
	f.Add("UPPER")

	f.Fuzz(func(t *testing.T, input string) {
		name, err := ParseInstanceName(input)
		if err != nil {
			return
		}
		value := name.String()
		if len(value) < 1 || len(value) > 32 || value[0] < 'a' || value[0] > 'z' {
			t.Fatalf("accepted invalid instance %q", value)
		}
		for index := 1; index < len(value); index++ {
			character := value[index]
			if !((character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '-') {
				t.Fatalf("accepted invalid character in %q", value)
			}
		}
	})
}
