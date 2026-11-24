// Copyright (c) 2025-2026 fei_cong(https://github.com/feicong/feicong-course)

package install

import (
	"testing"

	"golang.org/x/sys/unix"
)

func hookRename(t *testing.T, gate *intrGate) {
	t.Helper()
	move := rawRenameat2
	t.Cleanup(func() { rawRenameat2 = move })
	rawRenameat2 = func(fromFd int, from string, toFd int, to string, flags uint) error {
		if !gate.pass("rename") {
			return unix.EINTR
		}
		return move(fromFd, from, toFd, to, flags)
	}
}
