//go:build wasip1

package main

// wasip1 (WebAssembly/WASI, e.g. a-Shell's `wasm` command) has no termios and
// no select(2). Raw-mode reads are unavailable, so liner falls back to its
// line-based reader; these helpers are best-effort no-ops.

const iutf8Flag = 0

// restoreOutputFlags is a no-op: there is no tty to reconfigure under WASI.
func restoreOutputFlags(fd int) {}

// stdinReadable reports "readable" so the caller performs a blocking Read.
func stdinReadable(fd, usec int) int { return 1 }
