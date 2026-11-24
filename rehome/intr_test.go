// Copyright (c) 2025-2026 fei_cong(https://github.com/feicong/feicong-course)

package rehome

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"golang.org/x/sys/unix"
)

// intrGate让每个原始调用先收到一次EINTR，下一次才执行真实系统调用。
type intrGate struct {
	mu   sync.Mutex
	hits map[string]int
}

func (gate *intrGate) pass(name string) bool {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	gate.hits[name]++
	return gate.hits[name]%2 == 0
}

func (gate *intrGate) count(name string) int {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	return gate.hits[name]
}

func hookIntr(t *testing.T) *intrGate {
	t.Helper()
	gate := &intrGate{hits: map[string]int{}}
	openat, fstatat, mkdirat := rawOpenat, rawFstatat, rawMkdirat
	unlinkat, readlinkat := rawUnlinkat, rawReadlinkat
	t.Cleanup(func() {
		rawOpenat, rawFstatat, rawMkdirat = openat, fstatat, mkdirat
		rawUnlinkat, rawReadlinkat = unlinkat, readlinkat
	})
	rawOpenat = func(dir int, name string, flags int, mode uint32) (int, error) {
		if !gate.pass("openat") {
			return -1, unix.EINTR
		}
		return openat(dir, name, flags, mode)
	}
	rawFstatat = func(dir int, name string, stat *unix.Stat_t, flags int) error {
		if !gate.pass("fstatat") {
			return unix.EINTR
		}
		return fstatat(dir, name, stat, flags)
	}
	rawMkdirat = func(dir int, name string, mode uint32) error {
		if !gate.pass("mkdirat") {
			return unix.EINTR
		}
		return mkdirat(dir, name, mode)
	}
	rawUnlinkat = func(dir int, name string, flags int) error {
		if !gate.pass("unlinkat") {
			return unix.EINTR
		}
		return unlinkat(dir, name, flags)
	}
	rawReadlinkat = func(dir int, name string, buffer []byte) (int, error) {
		if !gate.pass("readlinkat") {
			return -1, unix.EINTR
		}
		return readlinkat(dir, name, buffer)
	}
	hookRename(t, gate)
	return gate
}

// 迁移旧组件时每个原始调用都先被打断一次，暂存、核对、读取链接与清理仍成功。
func TestMoveStepIntr(t *testing.T) {
	base := t.TempDir()
	bin := filepath.Join(base, "old", "tool", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	want := []byte("go version go1.26.8\n")
	if err := os.WriteFile(filepath.Join(bin, "go"), want, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("bin", filepath.Join(base, "old", "tool", "link")); err != nil {
		t.Fatal(err)
	}
	root, err := os.Open(base)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	gate := hookIntr(t)

	stage, token, err := makeStage(root)
	if err != nil {
		t.Fatalf("make stage: %v", err)
	}
	defer stage.Close()
	old, err := openDir(root, "old")
	if err != nil {
		t.Fatalf("open legacy directory: %v", err)
	}
	defer old.Close()
	if err := renameDir(old, stage, "tool"); err != nil {
		t.Fatalf("retire component: %v", err)
	}
	tool, err := openDir(stage, "tool")
	if err != nil {
		t.Fatalf("open retired component: %v", err)
	}
	defer tool.Close()
	if !sameChild(stage, tool, "tool") {
		t.Fatal("retired component identity was not confirmed")
	}
	if got, err := linkText(tool, "link"); err != nil || got != "bin" {
		t.Fatalf("link target=%q err=%v, want bin", got, err)
	}
	dir, err := openDir(tool, "bin")
	if err != nil {
		t.Fatalf("open retired bin: %v", err)
	}
	file, err := openFile(dir, "go")
	dir.Close()
	if err != nil {
		t.Fatalf("open retired file: %v", err)
	}
	got, err := io.ReadAll(file)
	file.Close()
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("content=%q err=%v, want %q", got, err, want)
	}
	if err := clearTree(tool); err != nil {
		t.Fatalf("clear retired component: %v", err)
	}
	if err := unlinkAt(int(stage.Fd()), "tool", unix.AT_REMOVEDIR); err != nil {
		t.Fatalf("remove retired component: %v", err)
	}
	items, err := os.ReadDir(filepath.Join(base, token))
	if err != nil || len(items) != 0 {
		t.Fatalf("stage entries=%v err=%v, want empty", items, err)
	}
	if _, err := os.Lstat(filepath.Join(base, "old", "tool")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy component remains: %v", err)
	}
	for _, call := range []string{"openat", "fstatat", "mkdirat", "unlinkat", "readlinkat", "rename"} {
		if got := gate.count(call); got < 2 {
			t.Fatalf("%s was not interrupted and retried: calls=%d", call, got)
		}
	}
}

// 其它错误原样返回，不进入重试。
func TestIntrKeepsErr(t *testing.T) {
	calls := 0
	unlinkat := rawUnlinkat
	t.Cleanup(func() { rawUnlinkat = unlinkat })
	rawUnlinkat = func(int, string, int) error {
		calls++
		return unix.ENOTEMPTY
	}
	if err := unlinkAt(-1, "busy", unix.AT_REMOVEDIR); !errors.Is(err, unix.ENOTEMPTY) {
		t.Fatalf("error=%v, want ENOTEMPTY", err)
	}
	if calls != 1 {
		t.Fatalf("unlinkat calls=%d, want 1", calls)
	}
}
