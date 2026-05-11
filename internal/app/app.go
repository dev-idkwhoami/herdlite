package app

import (
	"fmt"
	"io"

	"herdlite/internal/paths"
	"herdlite/internal/state"
)

type App struct {
	Out   io.Writer
	Err   io.Writer
	Paths paths.Paths
	Store *state.Store
}

func New(out io.Writer, errOut io.Writer) (*App, error) {
	p, resolveErr := paths.Resolve()
	if resolveErr != nil {
		return nil, resolveErr
	}

	return &App{
		Out:   out,
		Err:   errOut,
		Paths: p,
		Store: state.NewStore(p.StateFile),
	}, nil
}

func (a *App) InitUserDirs() error {
	if err := a.Paths.EnsureUserDirs(); err != nil {
		return fmt.Errorf("create herdlite directories: %w", err)
	}
	return nil
}
