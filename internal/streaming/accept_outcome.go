package streaming

import "github.com/abyssbugg/ant-router/internal/canonical"

type acceptOutcome struct {
	semantic bool
	terminal TerminalKind
	response *canonical.Response
	failure  *canonical.Failure
	usage    *canonical.Usage
}
