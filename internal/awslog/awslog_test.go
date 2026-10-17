package awslog

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/aws/smithy-go/logging"
	"github.com/stretchr/testify/assert"
)

func TestLogger(t *testing.T) {
	orig := slog.Default()

	t.Cleanup(func() { slog.SetDefault(orig) })

	buf := &bytes.Buffer{}
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelWarn})))

	l := Logger{}
	l.Logf(logging.Debug, "debug %s", "hidden")
	assert.Empty(t, buf.String())

	l.Logf(logging.Warn, "careful %d", 42)
	assert.Contains(t, buf.String(), "level=WARN")
	assert.Contains(t, buf.String(), `msg="careful 42"`)
	assert.Contains(t, buf.String(), "source=aws-sdk")

	buf.Reset()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	l.Logf(logging.Debug, "shown")
	assert.Contains(t, buf.String(), "level=DEBUG")
}
