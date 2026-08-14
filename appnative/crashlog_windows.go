//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
)

// Making the application unable to die silently.
//
// FlashFit is linked as a GUI binary, which on Windows means it starts with no
// console and GetStdHandle(STD_ERROR_HANDLE) returns nothing. The Go runtime
// writes an unrecovered panic — and the "out of memory" message — to that
// handle and nowhere else. With no console attached, both go into the void and
// the window simply vanishes: no dialog, no log line, nothing to work from.
//
// That is exactly what happened to the crash reported after switching printers.
// The window procedures all recover and log, and the log held no PANIC line, so
// whatever killed it was either a panic in a background goroutine or the
// runtime running out of memory — the two failures that were guaranteed to
// leave no trace.
//
// Two things close that off. Every goroutine that touches user data recovers
// and logs, and the process's own stderr is pointed at the log file, so
// anything the runtime reports on the way down is written down before the
// process goes. A crash that names itself is a bug with a fix; a window that
// disappears is a bug report nobody can act on.

var (
	pSetStdHandle = kernel32.NewProc("SetStdHandle")
	// Held for the life of the process: closing it would take the redirect with
	// it, and the redirect has to outlive everything that might crash.
	crashLogFile *os.File
)

const stdErrorHandle = ^uintptr(11) // STD_ERROR_HANDLE (-12)

// captureRuntimeFailures points the process's error output at the log, so the
// runtime's own last words end up somewhere readable.
func captureRuntimeFailures() {
	path := logPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	crashLogFile = f
	os.Stderr = f
	// The runtime asks Windows for the error handle on every write rather than
	// caching it, so redirecting it here also redirects the panic printer.
	pSetStdHandle.Call(stdErrorHandle, f.Fd())
}

// guard runs a background task so that a panic inside it is written down and
// contained, instead of taking the whole application with it.
//
// A panic in a goroutine is not recoverable from anywhere else: Go tears the
// process down immediately. So the recover has to be inside the goroutine, and
// every goroutine that reads a user's file or talks to the model needs one —
// those are the ones handling input the code has not seen before.
func guard(label string, task func()) {
	defer func() {
		if r := recover(); r != nil {
			writeLog(fmt.Sprintf("PANIC %s: %v\n%s", label, r, debug.Stack()))
		}
	}()
	task()
}
