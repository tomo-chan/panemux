// Package debuglog provides opt-in diagnostic logging for investigation builds.
package debuglog

import (
	"log"
	"sync"
	"sync/atomic"
)

var (
	enabled atomic.Bool

	loggerMu sync.RWMutex
	logger   = log.Default()
)

func SetEnabled(v bool) {
	enabled.Store(v)
}

func Enabled() bool {
	return enabled.Load()
}

func SetLogger(l *log.Logger) {
	loggerMu.Lock()
	defer loggerMu.Unlock()
	if l == nil {
		logger = log.Default()
		return
	}
	logger = l
}

func Debugf(format string, args ...any) {
	if !Enabled() {
		return
	}
	loggerMu.RLock()
	defer loggerMu.RUnlock()
	logger.Printf("gitinfo-debug: "+format, args...)
}
