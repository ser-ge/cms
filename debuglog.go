package main

import (
	"fmt"
	"log"
	"os"
	"sync"

	"github.com/serge/cms/internal/debug"
)

var (
	debugLogOnce sync.Once
	debugLogger  *log.Logger
)

func initDebugLogger() {
	debugLogOnce.Do(func() {
		if os.Getenv("CMS_DEBUG") == "" {
			return
		}

		path := os.Getenv("CMS_DEBUG_LOG")
		if path == "" {
			path = "/tmp/cms-debug.log"
		}

		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "debug log open error: %v\n", err)
			return
		}
		debugLogger = log.New(f, "", log.LstdFlags|log.Lmicroseconds)
		debug.Logf = debugLogger.Printf
		debugLogger.Printf("debug logging enabled")
	})
}

func debugf(format string, args ...any) {
	if debugLogger == nil {
		return
	}
	debugLogger.Printf(format, args...)
}
