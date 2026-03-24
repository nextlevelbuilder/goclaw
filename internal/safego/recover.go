// Package safego provides panic-recovery helpers for goroutines.
package safego

import (
	"fmt"
	"log/slog"
	"runtime"
)

// Recover catches panics, logs an error with stack trace, and optionally
// invokes onPanic. Must be called via defer:
//
//	defer safego.Recover(nil, "job_id", id)              // log-only
//	defer safego.Recover(func(v any) { ... }, "tool", n) // log + callback
func Recover(onPanic func(v any), attrs ...any) {
	r := recover()
	if r == nil {
		return
	}
	buf := make([]byte, 8192)
	n := runtime.Stack(buf, false)
	slog.Error("goroutine panicked",
		append(attrs, "panic", fmt.Sprint(r), "stack", string(buf[:n]))...,
	)
	if onPanic != nil {
		onPanic(r)
	}
}
