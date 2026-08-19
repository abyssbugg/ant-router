package control

import "testing"

func FuzzStrictControlJSON(f *testing.F) {
	f.Add([]byte(`{"status":"ok","api_version":"v1"}`))
	f.Add([]byte(`{"status":"ok","api_version":"v1"} {}`))
	f.Add([]byte(`{"error":{"code":"unauthorized","message":"authorization failed","request_id":"r"}}`))
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		var health HealthResponse
		if err := strictDecodeJSON(data, &health); err == nil {
			_ = validateHealthResponse(health)
		}
	})
}
