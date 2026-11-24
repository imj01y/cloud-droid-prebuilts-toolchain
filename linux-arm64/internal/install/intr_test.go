// Copyright (c) 2025-2026 fei_cong(https://github.com/feicong/feicong-course)

package install

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
	fchmod, symlinkat := rawFchmod, rawSymlinkat
	t.Cleanup(func() {
		rawOpenat, rawFstatat, rawMkdirat = openat, fstatat, mkdirat
		rawUnlinkat, rawReadlinkat = unlinkat, readlinkat
		rawFchmod, rawSymlinkat = fchmod, symlinkat
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
	rawFchmod = func(fd int, mode uint32) error {
		if !gate.pass("fchmod") {
			return unix.EINTR
		}
		return fchmod(fd, mode)
	}
	rawSymlinkat = func(target string, dir int, name string) error {
		if !gate.pass("symlinkat") {
			return unix.EINTR
		}
		return symlinkat(target, dir, name)
	}
	hookRename(t, gate)
	return gate
}

func openBase(t *testing.T) (string, *os.File) {
	t.Helper()
	base := t.TempDir()
	root, err := os.Open(base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	return base, root
}

func readRel(t *testing.T, root *os.File, rel string) []byte {
	t.Helper()
	file, err := openRel(root, rel, true)
	if err != nil {
		t.Fatalf("open %s: %v", rel, err)
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// 工具链检查打开bin/cargo时openat被信号打断，应重试而不是报告无效探针。
func TestOpenRelIntr(t *testing.T) {
	base, root := openBase(t)
	bin := filepath.Join(base, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	want := []byte("cargo 1.97.1\n")
	if err := os.WriteFile(filepath.Join(bin, "cargo"), want, 0o755); err != nil {
		t.Fatal(err)
	}
	calls := 0
	openat := rawOpenat
	t.Cleanup(func() { rawOpenat = openat })
	rawOpenat = func(dir int, name string, flags int, mode uint32) (int, error) {
		calls++
		if calls%2 == 1 {
			return -1, unix.EINTR
		}
		return openat(dir, name, flags, mode)
	}
	if got := readRel(t, root, "bin/cargo"); !bytes.Equal(got, want) {
		t.Fatalf("content=%q, want %q", got, want)
	}
	if calls != 4 {
		t.Fatalf("openat calls=%d, want one retry for each of 2 components", calls)
	}
}

// 每个原始调用都先被打断一次时，建目录、写文件、跟随链接、清理和重命名仍成功。
func TestSysCallIntr(t *testing.T) {
	base, root := openBase(t)
	gate := hookIntr(t)
	dir, err := makeDir(root, "tool/bin")
	if err != nil {
		t.Fatalf("make directory: %v", err)
	}
	dir.Close()
	want := []byte("rustc 1.97.1\n")
	file, err := newFile(root, "tool/bin/rustc", 0o755)
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	if _, err := file.Write(want); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	assertMode(t, filepath.Join(base, "tool"), 0o755)
	assertMode(t, filepath.Join(base, "tool/bin/rustc"), 0o755)
	if err := newLink(root, "tool/link", "bin"); err != nil {
		t.Fatalf("create link: %v", err)
	}
	if got := readRel(t, root, "tool/link/rustc"); !bytes.Equal(got, want) {
		t.Fatalf("linked content=%q, want %q", got, want)
	}
	id, err := nodeAt(root, "tool/bin/rustc")
	if err != nil {
		t.Fatalf("inspect node: %v", err)
	}
	var stat unix.Stat_t
	if err := unix.Lstat(filepath.Join(base, "tool/bin/rustc"), &stat); err != nil {
		t.Fatal(err)
	}
	if id.inode != uint64(stat.Ino) || id.device != uint64(stat.Dev) {
		t.Fatalf("node=%+v, want dev=%d ino=%d", id, stat.Dev, stat.Ino)
	}

	work, name, err := makeTemp(root, "stage-")
	if err != nil {
		t.Fatalf("make stage: %v", err)
	}
	nested, err := makeDir(work, "nested")
	if err != nil {
		t.Fatalf("make nested: %v", err)
	}
	nested.Close()
	item, err := newFile(work, "nested/item", 0o644)
	if err != nil {
		t.Fatalf("create nested item: %v", err)
	}
	item.Close()
	clearTemp(root, work, name)
	work.Close()
	if _, err := os.Lstat(filepath.Join(base, name)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stage remains after cleanup: %v", err)
	}

	if err := moveAs(root, "tool", "moved"); err != nil {
		t.Fatalf("move without replacement: %v", err)
	}
	other, err := makeDir(root, "other")
	if err != nil {
		t.Fatal(err)
	}
	other.Close()
	if err := swapAt(root, "moved", "other"); err != nil {
		t.Fatalf("swap directories: %v", err)
	}
	if got := readRel(t, root, "other/bin/rustc"); !bytes.Equal(got, want) {
		t.Fatalf("swapped content=%q, want %q", got, want)
	}
	if err := swapDirs(filepath.Join(base, "other"), filepath.Join(base, "moved")); err != nil {
		t.Fatalf("swap paths: %v", err)
	}
	if got := readRel(t, root, "moved/bin/rustc"); !bytes.Equal(got, want) {
		t.Fatalf("restored content=%q, want %q", got, want)
	}
	for _, call := range []string{
		"openat", "fstatat", "mkdirat", "unlinkat", "readlinkat", "fchmod", "symlinkat", "rename",
	} {
		if got := gate.count(call); got < 2 {
			t.Fatalf("%s was not interrupted and retried: calls=%d", call, got)
		}
	}
}

// 其它错误原样返回，不进入重试。
func TestIntrKeepsErr(t *testing.T) {
	calls := 0
	openat := rawOpenat
	t.Cleanup(func() { rawOpenat = openat })
	rawOpenat = func(int, string, int, uint32) (int, error) {
		calls++
		return -1, unix.ENOENT
	}
	if _, err := openAt(-1, "missing", unix.O_RDONLY, 0); !errors.Is(err, unix.ENOENT) {
		t.Fatalf("error=%v, want ENOENT", err)
	}
	if calls != 1 {
		t.Fatalf("openat calls=%d, want 1", calls)
	}
}
