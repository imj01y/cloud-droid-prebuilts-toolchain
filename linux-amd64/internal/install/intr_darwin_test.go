// Copyright (c) 2025-2026 fei_cong(https://github.com/feicong/feicong-course)

package install

import (
	"testing"

	"golang.org/x/sys/unix"
)

func hookRename(t *testing.T, gate *intrGate) {
	t.Helper()
	swap, move := rawRenamexNp, rawRenameatxNp
	t.Cleanup(func() { rawRenamexNp, rawRenameatxNp = swap, move })
	rawRenamexNp = func(from, to string, flag uint32) error {
		if !gate.pass("rename") {
			return unix.EINTR
		}
		return swap(from, to, flag)
	}
	rawRenameatxNp = func(fromFd int, from string, toFd int, to string, flag uint32) error {
		if !gate.pass("rename") {
			return unix.EINTR
		}
		return move(fromFd, from, toFd, to, flag)
	}
}
