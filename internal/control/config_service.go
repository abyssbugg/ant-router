package control

import (
	"context"

	"github.com/abyssbugg/ant-router/internal/configservice"
)

type ConfigService interface {
	Validate(context.Context, []byte) (configservice.ValidationResult, error)
	Apply(context.Context, configservice.ApplyRequest) (configservice.CurrentResult, error)
	Current() configservice.CurrentResult
	Rollback(context.Context) (configservice.CurrentResult, error)
}
