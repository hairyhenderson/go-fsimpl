// Package awslog adapts the AWS SDK's logger to [log/slog], so that SDK
// messages follow the application's configured log level and format instead of
// being written straight to stderr.
//
// Unusually, smithy-go's logging package only defines two classifications,
// [logging.Warn] and [logging.Debug]; there are no info or error levels.
// Anything that isn't a warning is treated as debug.
package awslog

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/aws/smithy-go/logging"
)

// Logger is a [logging.Logger] that writes to the default [slog.Logger]. The
// logger is looked up on each call, so later changes to [slog.SetDefault] are
// respected.
type Logger struct{}

var _ logging.Logger = Logger{}

// Logf implements [logging.Logger]. [logging.Warn] maps to [slog.LevelWarn],
// and everything else (in practice, [logging.Debug]) to [slog.LevelDebug].
func (Logger) Logf(classification logging.Classification, format string, v ...any) {
	level := slog.LevelDebug
	if classification == logging.Warn {
		level = slog.LevelWarn
	}

	l := slog.Default()
	ctx := context.Background()

	if !l.Enabled(ctx, level) {
		return
	}

	l.Log(ctx, level, fmt.Sprintf(format, v...), slog.String("source", "aws-sdk"))
}
