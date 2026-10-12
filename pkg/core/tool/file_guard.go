package tool

import (
	"context"

	"github.com/jiaweisky/go-agent-sdk/pkg/core"
)

// guardFileWrite consults the optional file-write guard for one target. A nil
// guard allows every write.
func guardFileWrite(ctx context.Context, guard core.FileWriteGuard, path string, op core.FileOp) error {
	if guard == nil {
		return nil
	}
	return guard.Allow(ctx, path, op)
}
