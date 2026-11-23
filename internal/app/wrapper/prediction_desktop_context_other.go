//go:build !darwin

package wrapper

import (
	"context"
	"errors"
)

func readPredictionDesktopContext(ctx context.Context, accountID string) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, errors.New("desktop prediction context is only available on macOS")
}
