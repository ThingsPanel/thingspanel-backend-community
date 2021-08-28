package log

import (
	"context"
	"github.com/ThingsPanel/ThingsPanel-Go/internal/adaptors"
	"github.com/google/uuid"
	"github.com/tryfix/log"
	traceableCtx "github.com/tryfix/traceable-context"
)

func DefaultLog(appDebug bool) log.Logger {
	logger := log.NewLog(log.WithOutput(log.OutJson))

	// when in debug mode use the text logger
	if appDebug {
		logger = log.NewLog(log.WithOutput(log.OutText), log.WithColors(true))
	}

	l := logger.Log(
		log.WithLevel(log.Level(adaptors.TRACE)),
		log.Prefixed(`application`),
		log.WithSkipFrameCount(3), // nolint
		log.WithFilePath(true),
		log.WithCtxTraceExtractor(func(ctx context.Context) string {
			if trace := traceableCtx.FromContext(ctx); trace != uuid.Nil {
				return trace.String()
			}
			return ""
		}))

	return l
}
