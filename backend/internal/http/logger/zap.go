package logger

import (
	"go.uber.org/zap"
)

type ZapLogger struct {
	logger *zap.Logger
}

func (l *ZapLogger) Write(p []byte) (int, error) {
	l.logger.Error(string(p))

	return len(p), nil
}

func NewZap(l *zap.Logger) *ZapLogger {
	return &ZapLogger{l}
}
