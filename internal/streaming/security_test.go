package streaming

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

func TestStreamingErrorsDoNotLeakContentOrBoundaryErrors(t *testing.T) {
	const secret = "secret-payload-never-log"
	b := newStreamBuilder(t, DefaultMachineOptions())
	itemID := streamItemID(t, 1)
	start, _ := canonical.NewItemStartedEvent(b.nextEventID(), b.responseID, b.sequence, canonical.ItemStartParams{Index: 0, ID: itemID, Type: canonical.ItemType("message"), Role: canonical.Role("assistant")})
	b.sequence++
	b.mustAccept(start)
	content, _ := canonical.NewContentStartedEvent(b.nextEventID(), b.responseID, b.sequence, itemID, 0, canonical.ContentType("text"))
	b.sequence++
	b.mustAccept(content)
	delta, _ := canonical.NewTextDeltaEvent(b.nextEventID(), b.responseID, b.sequence, itemID, 0, secret)
	b.sequence++
	b.mustAccept(delta)
	wrong, _ := canonical.NewTextContent("different")
	block, _ := canonical.NewTextBlock(wrong)
	completed, _ := canonical.NewContentCompletedEvent(b.nextEventID(), b.responseID, b.sequence, itemID, 0, block)
	_, err := b.machine.Accept(completed)
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("machine error leaked content: %v", err)
	}

	source := &scriptedSource{events: loadGoldenEvents(t)[:1], terminal: errors.New(secret)}
	result := newTestPump(t, source, &recordingSink{failAt: -1}, DefaultPumpOptions()).Run(context.Background())
	if result.Cause() == nil || strings.Contains(result.Cause().Error(), secret) {
		t.Fatalf("pump error leaked boundary content: %v", result.Cause())
	}
}
