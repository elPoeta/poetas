package provider

import (
	"context"

	"github.com/elPoeta/poetas/internal/api"
)

type Provider interface {
	Send(ctx context.Context, messages []api.Message, tools []api.ToolDef) (api.Response, error)
	Model() string
	SetModel(name string)
}
