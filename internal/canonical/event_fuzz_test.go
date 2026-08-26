package canonical

import "testing"

func FuzzTextDelta(f *testing.F) {
	f.Add("hello")
	f.Add("")
	f.Fuzz(func(t *testing.T, delta string) {
		event, err := NewTextDeltaEvent(testEventID(t, 1), testResponseID(t, 1), 0, testItemID(t, 1), 0, delta)
		if err != nil {
			return
		}
		if !event.Valid() || event.Type() != EventType("text_delta") {
			t.Fatal("accepted inconsistent text delta")
		}
	})
}
