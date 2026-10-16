//go:build darwin

package main

import "golang.org/x/sys/unix"

const (
	termiosGet = unix.TIOCGETA
	termiosSet = unix.TIOCSETA
	iutf8Flag  = 0 // macOS: no IUTF8, terminal handles UTF-8 natively
)
