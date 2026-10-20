//go:build e2e

package e2e

import "testing"

func TestDaemonFailureReportOnlyOnFailureWithOutput(t *testing.T) {
	for _, tc := range []struct {
		name   string
		failed bool
		stderr string
		want   string
	}{
		{"passed with output", false, "http: panic serving", ""},
		{"failed without output", true, "", ""},
		{"failed with output", true, "http: panic serving\n", "routerd (pid 42) stderr:\nhttp: panic serving\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := daemonFailureReport(tc.failed, 42, []byte(tc.stderr)); got != tc.want {
				t.Errorf("daemonFailureReport() = %q, want %q", got, tc.want)
			}
		})
	}
}
