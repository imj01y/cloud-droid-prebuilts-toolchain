// Copyright (c) 2025-2026 fei_cong(https://github.com/feicong/feicong-course)

package install

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"

	"golang.org/x/sys/unix"
)

// 原始重命名调用，测试替换它们以注入被信号打断的结果。
var (
	rawRenamexNp   = unix.RenamexNp
	rawRenameatxNp = unix.RenameatxNp
)

func swapDirs(left, right string) error {
	return noIntr(func() error { return rawRenamexNp(left, right, unix.RENAME_SWAP) })
}

func swapAt(root *os.File, left, right string) error {
	return renameAt(int(root.Fd()), left, int(root.Fd()), right, unix.RENAME_SWAP)
}

func moveNew(from *os.File, name string, root *os.File) error {
	return renameAt(int(from.Fd()), name, int(root.Fd()), name, unix.RENAME_EXCL)
}

func moveAs(root *os.File, from, dest string) error {
	return renameAt(int(root.Fd()), from, int(root.Fd()), dest, unix.RENAME_EXCL)
}

func renameAt(fromFd int, from string, toFd int, to string, flag uint32) error {
	return noIntr(func() error { return rawRenameatxNp(fromFd, from, toFd, to, flag) })
}

func fdAlias(file *os.File) string {
	return "/dev/fd/" + strconv.FormatUint(uint64(file.Fd()), 10)
}

func runAt(ctx context.Context, root, program *os.File, rel string, _ bool, args, env []string) ([]byte, error) {
	_ = ctx.Done()
	doneCtx := ctx.Done()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	parent, name, err := openParent(root, program.Name(), false)
	if err != nil {
		return nil, fmt.Errorf("open probe parent: %w", err)
	}
	defer parent.Close()
	runCtx, cancel := context.WithCancel(context.Background())
	stop := make(chan struct{})
	if doneCtx != nil {
		go func() {
			select {
			case <-doneCtx:
				cancel()
			case <-stop:
			}
		}()
	}
	defer func() {
		close(stop)
		cancel()
	}()
	type result struct {
		output []byte
		err    error
	}
	done := make(chan result, 1)
	go func() {
		runtime.LockOSThread()
		if err := noIntr(func() error { return unix.PthreadFchdir(int(parent.Fd())) }); err != nil {
			runtime.UnlockOSThread()
			done <- result{err: fmt.Errorf("set probe directory: %w", err)}
			return
		}
		got := result{}
		if !sameChild(parent, program, name) {
			got.err = errors.New("probe changed before execution")
		} else {
			cmd := exec.CommandContext(runCtx, "./"+name, args...)
			cmd.Args[0] = rel
			cmd.Env = env
			got.output, got.err = cmd.CombinedOutput()
		}
		if err := noIntr(func() error { return unix.PthreadFchdir(-1) }); err != nil {
			got.err = errors.Join(got.err, fmt.Errorf("restore probe directory: %w", err))
			done <- got
			return
		}
		runtime.UnlockOSThread()
		done <- got
	}()
	got := <-done
	if err := ctx.Err(); err != nil {
		got.err = errors.Join(got.err, err)
	}
	return got.output, got.err
}
