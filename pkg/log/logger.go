package log

import (
	"context"
	"github.com/ThingsPanel/ThingsPanel-Go/internal/adaptors"
	"github.com/kosatnkn/veil"
	"github.com/tryfix/log"
)

type Log struct {
	logger   log.Logger
	obscurer veil.Veil
}

func (l *Log) Init(defaultLog log.Logger) error {
	l.logger = defaultLog
	// define global rules for obscurer
	var rules []veil.Rule
	rules = append(rules, veil.NewRule("email", veil.PatternEmail, veil.ActionMaskFunc))

	obs, err := veil.NewVeil(rules)
	if err != nil {
		return err
	}
	l.obscurer = obs

	return nil
}

func (l *Log) NewLog(options ...adaptors.LoggerOption) adaptors.Logger {
	optMap := adaptors.NewLoggerOptions()
	for _, opt := range options {
		opt(optMap)
	}

	var opts []log.Option
	for typ, opt := range optMap {
		switch typ {
		case `prefix`:
			opts = append(opts, log.Prefixed(opt.(string)))
		case `level`:
			opts = append(opts, log.WithLevel(log.Level(opt.(adaptors.LogLevel))))
		}
	}

	return newLogger(l.logger.NewLog(opts...))
}

func (l *Log) AddObscureRules(rules []adaptors.LoggerObscureRule) error {
	rls := l.obscurer.Rules()

	for _, rl := range rules {
		rls = append(rls, veil.NewRule(rl.Name, rl.Pattern, rl.Action))
	}

	obs, err := veil.NewVeil(rls)
	if err != nil {
		return err
	}
	l.obscurer = obs

	return nil
}

func (l *Log) Obscure(inputs ...interface{}) ([]interface{}, error) {
	return l.obscurer.Process(inputs...)
}

func (l *Log) Params(key, value string) string {
	return key + ":" + value
}

func (l *Log) Fatal(message interface{}, params ...interface{}) {
	l.logger.Fatal(message, params...)
}

func (l *Log) Error(message interface{}, params ...interface{}) {
	l.logger.Error(message, params...)
}

func (l *Log) Warn(message interface{}, params ...interface{}) {
	l.logger.Warn(message, params...)
}

func (l *Log) Debug(message interface{}, params ...interface{}) {
	l.logger.Debug(message, params...)
}

func (l *Log) Info(message interface{}, params ...interface{}) {
	l.logger.Info(message, params...)
}

func (l *Log) Trace(message interface{}, params ...interface{}) {
	l.logger.Trace(message, params...)
}

func (l *Log) FatalContext(ctx context.Context, message interface{}, params ...interface{}) {
	l.logger.FatalContext(ctx, message, params...)
}

func (l *Log) ErrorContext(ctx context.Context, message interface{}, params ...interface{}) {
	l.logger.ErrorContext(ctx, message, params...)
}

func (l *Log) WarnContext(ctx context.Context, message interface{}, params ...interface{}) {
	l.logger.WarnContext(ctx, message, params...)
}

func (l *Log) DebugContext(ctx context.Context, message interface{}, params ...interface{}) {
	l.logger.DebugContext(ctx, message, params...)
}

func (l *Log) InfoContext(ctx context.Context, message interface{}, params ...interface{}) {
	l.logger.InfoContext(ctx, message, params...)
}

func (l *Log) TraceContext(ctx context.Context, message interface{}, params ...interface{}) {
	l.logger.TraceContext(ctx, message, params...)
}

func (l *Log) Print(v ...interface{}) {
	l.logger.Print(v...)
}

func (l *Log) Printf(format string, v ...interface{}) {
	l.logger.Printf(format, v...)
}

func (l *Log) Println(v ...interface{}) {
	l.logger.Println(v...)
}

func newLogger(log log.Logger) adaptors.Logger {
	return &Log{logger: log}
}
