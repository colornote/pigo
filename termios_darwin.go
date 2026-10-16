//go:build darwin

package main

import "golang.org/x/sys/unix"

const (
	termiosGet = unix.TIOCGETA
	termiosSet = unix.TIOCSETA
	iutf8Flag  = 0 // macOS: no IUTF8, terminal handles UTF-8 natively
)

// restoreOutputFlags re-enables OPOST/ONLCR (so \n → \r\n) after MakeRaw
// cleared them. No-op if the ioctl fails (e.g. stdin is not a tty).
func restoreOutputFlags(fd int) {
	if tios, err := unix.IoctlGetTermios(fd, termiosGet); err == nil {
		tios.Oflag |= unix.OPOST | unix.ONLCR
		unix.IoctlSetTermios(fd, termiosSet, tios)
	}
}

// stdinReadable waits up to usec microseconds for fd to become readable.
// Returns >0 if readable, 0 on timeout, <0 on error.
func stdinReadable(fd, usec int) int {
	var rset unix.FdSet
	rset.Set(fd)
	tv := unix.NsecToTimeval(int64(usec) * 1000)
	n, _ := unix.Select(fd+1, &rset, nil, nil, &tv)
	return n
}
