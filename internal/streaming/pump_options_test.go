package streaming

import (
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

func TestDefaultPumpOptions(t *testing.T) {
	options := DefaultPumpOptions()
	if options.MaxPrecommitEvents != 256 || options.MaxPrecommitBytes != canonical.MaxOrdinaryEventBytes || options.StallTimeout != 30*time.Second || !options.Valid() {
		t.Fatalf("options=%#v", options)
	}
}

func TestPumpOptionsRejectNonpositiveValues(t *testing.T) {
	cases := []PumpOptions{
		{MaxPrecommitEvents: 0, MaxPrecommitBytes: 1, StallTimeout: time.Second},
		{MaxPrecommitEvents: 1, MaxPrecommitBytes: 0, StallTimeout: time.Second},
		{MaxPrecommitEvents: 1, MaxPrecommitBytes: 1, StallTimeout: 0},
	}
	for _, options := range cases {
		if options.Valid() {
			t.Errorf("options accepted: %#v", options)
		}
	}
}
