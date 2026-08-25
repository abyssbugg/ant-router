package canonical

import "testing"

func TestProtocolVersionAndLimits(t *testing.T) {
	if ProtocolVersion != "1" {
		t.Errorf("ProtocolVersion = %q, want 1", ProtocolVersion)
	}
	tests := map[string]struct{ got, want int }{
		"request":      {MaxRequestBytes, 32 << 20},
		"items":        {MaxInputItems, 1024},
		"content":      {MaxContentBlocks, 256},
		"text":         {MaxTextBytes, 1 << 20},
		"json":         {MaxJSONBytes, 1 << 20},
		"depth":        {MaxJSONDepth, 64},
		"tools":        {MaxTools, 128},
		"tool schema":  {MaxToolSchemaBytes, 256 << 10},
		"image":        {MaxInlineImageBytes, 20 << 20},
		"image URL":    {MaxImageURLBytes, 2048},
		"dimension":    {MaxImageDimension, 32768},
		"continuation": {MaxContinuationBytes, 256 << 10},
		"event":        {MaxOrdinaryEventBytes, 2 << 20},
	}
	for name, test := range tests {
		if test.got != test.want {
			t.Errorf("%s limit = %d, want %d", name, test.got, test.want)
		}
	}
}
