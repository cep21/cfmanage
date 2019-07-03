package logger

import (
	"log"
	"sync"
)

// Logger helps us verbose log output
type Logger struct {
	Verbosity int
	Logger    *log.Logger
	mu        sync.Mutex
}

// Log will log.Printf the args if verbosity <= this logger's verbosity
func (l *Logger) Log(verbosity int, msg string, args ...interface{}) {
	if l == nil {
		return
	}
	if verbosity > l.Verbosity {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.Logger.Printf(msg, args...)
}
