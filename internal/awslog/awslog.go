// Package awslog adapts the AWS SDK's logger to [log/slog], so that SDK
// messages follow the application's configured log level and format instead of
// being written straight to stderr.
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

// Logf implements [logging.Logger].
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
