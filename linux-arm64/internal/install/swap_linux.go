// Copyright (c) 2025-2026 fei_cong(https://github.com/feicong/feicong-course)

package install

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"

	"golang.org/x/sys/unix"
)

// 原始重命名调用，测试替换它以注入被信号打断的结果。
var rawRenameat2 = unix.Renameat2

func swapDirs(left, right string) error {
	return renameAt(unix.AT_FDCWD, left, unix.AT_FDCWD, right, unix.RENAME_EXCHANGE)
}

func swapAt(root *os.File, left, right string) error {
	return renameAt(int(root.Fd()), left, int(root.Fd()), right, unix.RENAME_EXCHANGE)
}

func moveNew(from *os.File, name string, root *os.File) error {
	return renameAt(int(from.Fd()), name, int(root.Fd()), name, unix.RENAME_NOREPLACE)
}

func moveAs(root *os.File, from, dest string) error {
	return renameAt(int(root.Fd()), from, int(root.Fd()), dest, unix.RENAME_NOREPLACE)
}

func renameAt(fromFd int, from string, toFd int, to string, flags uint) error {
	return noIntr(func() error { return rawRenameat2(fromFd, from, toFd, to, flags) })
}

func fdAlias(file *os.File) string {
	return "/proc/self/fd/" + strconv.FormatUint(uint64(file.Fd()), 10)
}

func runAt(ctx context.Context, root, program *os.File, rel string, _ bool, args, env []string) ([]byte, error) {
	done := ctx.Done()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if done != nil {
		go func() {
			select {
			case <-done:
				cancel()
			case <-runCtx.Done():
			}
		}()
	}
	parent, leaf, err := openParent(root, program.Name(), false)
	if err != nil {
		return nil, fmt.Errorf("open probe parent %q: %w", rel, err)
	}
	defer parent.Close()

	cmd := exec.CommandContext(runCtx, "/proc/self/fd/3", args...)
	cmd.Args[0] = rel
	cmd.Env = env
	cmd.ExtraFiles = []*os.File{program}
	if !sameChild(parent, program, leaf) {
		return nil, fmt.Errorf("probe path changed before execution: %q", rel)
	}
	output, runErr := cmd.CombinedOutput()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return output, errors.Join(runErr, ctxErr)
	}
	return output, runErr
}
