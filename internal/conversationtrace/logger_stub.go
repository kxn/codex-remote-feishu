//go:build !devtrace

package conversationtrace

type Logger struct{}

func Open(path string) (*Logger, error) {
	return nil, nil
}

func Enabled() bool {
	return false
}

func (l *Logger) Close() error {
	return nil
}

func (l *Logger) Log(entry Entry) {}
