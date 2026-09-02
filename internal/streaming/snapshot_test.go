package streaming

import (
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

func TestSnapshotContainsOnlyStructuralMetadata(t *testing.T) {
	usage, _ := canonical.NewUsage(canonical.UsageParams{InputTokens: 1, OutputTokens: 2, TotalTokens: 3})
	snapshot := newSnapshot(StateCommitted, streamResponseID(t, 1), 7, 3, 2, 1, usage, true, 42, true, TerminalNone, 99)
	if snapshot.State() != StateCommitted || snapshot.NextSequence() != 7 || snapshot.StartedItems() != 3 || snapshot.CompletedItems() != 2 || snapshot.OpenContent() != 1 || !snapshot.HasContinuation() || snapshot.ContinuationBytes() != 42 || !snapshot.Committed() || snapshot.ReconstructedBytes() != 99 || snapshot.LatestUsage().TotalTokens() != 3 {
		t.Errorf("snapshot=%#v", snapshot)
	}
}
