//go:build !darwin

package main

import "golang.org/x/sys/unix"

const (
	termiosGet = unix.TCGETS
	termiosSet = unix.TCSETS
	iutf8Flag  = unix.IUTF8 // preserve multi-byte character backspace
)
