package streaming

import "github.com/abyssbugg/ant-router/internal/canonical"

type contentState struct {
	index     int
	kind      canonical.ContentType
	completed bool
}
