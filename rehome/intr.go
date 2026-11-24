// Copyright (c) 2025-2026 fei_cong(https://github.com/feicong/feicong-course)

package rehome

import (
	"errors"

	"golang.org/x/sys/unix"
)

// 原始文件系统调用，测试替换它们以注入被信号打断的结果。
var (
	rawOpenat     = unix.Openat
	rawFstatat    = unix.Fstatat
	rawMkdirat    = unix.Mkdirat
	rawUnlinkat   = unix.Unlinkat
	rawReadlinkat = unix.Readlinkat
)

// noIntr重复被信号打断的调用。x/sys/unix不像os包那样处理EINTR，
// Go运行时的抢占信号会让这些调用随机返回EINTR。
func noIntr(call func() error) error {
	for {
		err := call()
		if !errors.Is(err, unix.EINTR) {
			return err
		}
	}
}

func openAt(dir int, name string, flags int, mode uint32) (int, error) {
	fd := -1
	err := noIntr(func() error {
		var err error
		fd, err = rawOpenat(dir, name, flags, mode)
		return err
	})
	return fd, err
}

func fstatAt(dir int, name string, stat *unix.Stat_t, flags int) error {
	return noIntr(func() error { return rawFstatat(dir, name, stat, flags) })
}

func mkdirAt(dir int, name string, mode uint32) error {
	return noIntr(func() error { return rawMkdirat(dir, name, mode) })
}

func unlinkAt(dir int, name string, flags int) error {
	return noIntr(func() error { return rawUnlinkat(dir, name, flags) })
}

func readlinkAt(dir int, name string, buffer []byte) (int, error) {
	count := 0
	err := noIntr(func() error {
		var err error
		count, err = rawReadlinkat(dir, name, buffer)
		return err
	})
	return count, err
}
