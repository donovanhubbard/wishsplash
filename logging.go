package wishsplash

const LOG_PREFIX = "wishsplash:"

type Logger interface {
	Debug(any, ...any)
	Info(any, ...any)
	Warn(any, ...any)
	Error(any, ...any)
}

type noopLogger struct{}

func (n noopLogger) Debug(any, ...any) {}
func (n noopLogger) Info(any, ...any)  {}
func (n noopLogger) Warn(any, ...any)  {}
func (n noopLogger) Error(any, ...any) {}
