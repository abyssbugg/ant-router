package daemon

import (
	"context"

	"github.com/abyssbugg/ant-router/internal/control"
	"github.com/abyssbugg/ant-router/internal/controltoken"
	"github.com/abyssbugg/ant-router/internal/controltransport"
	"github.com/abyssbugg/ant-router/internal/dataplane"
	"github.com/abyssbugg/ant-router/internal/inferencetoken"
)

// liveControlVerifier verifies control-plane requests against the current
// on-disk control token, so a rotation takes effect without a daemon restart.
type liveControlVerifier struct {
	store    *controltoken.Store
	instance controltransport.InstanceName
}

func (v liveControlVerifier) Verify(raw string) bool {
	token, err := v.store.Load(v.instance)
	if err != nil {
		return false
	}
	return token.Verify(raw)
}

var _ control.TokenVerifier = liveControlVerifier{}

// liveInferenceTokenSource serves data-plane authentication from the current
// on-disk inference token, so a rotation takes effect without a daemon restart.
type liveInferenceTokenSource struct {
	store    *inferencetoken.Store
	instance controltransport.InstanceName
}

func (s liveInferenceTokenSource) Token(context.Context) (inferencetoken.Token, error) {
	return s.store.Load(s.instance)
}

var _ dataplane.TokenSource = liveInferenceTokenSource{}
