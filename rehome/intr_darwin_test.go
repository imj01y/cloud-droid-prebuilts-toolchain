// Copyright (c) 2025-2026 fei_cong(https://github.com/feicong/feicong-course)

package rehome

import (
	"testing"

	"golang.org/x/sys/unix"
)

func hookRename(t *testing.T, gate *intrGate) {
	t.Helper()
	move := rawRenameatxNp
	t.Cleanup(func() { rawRenameatxNp = move })
	rawRenameatxNp = func(fromFd int, from string, toFd int, to string, flag uint32) error {
		if !gate.pass("rename") {
			return unix.EINTR
		}
		return move(fromFd, from, toFd, to, flag)
	}
}
