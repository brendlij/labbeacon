package command

import (
	"context"
	"os/exec"
)

type Runner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}
type Exec struct{}

func (Exec) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}
