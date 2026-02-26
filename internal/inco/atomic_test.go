package inco

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestAtomicWriteFile_Basic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")

	data := []byte(`{"key": "value"}`)
	if err := atomicWriteFile(path, data, 0o644); err != nil {
		t.Fatalf("atomicWriteFile: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("content = %q, want %q", got, data)
	}
}

func TestAtomicWriteFile_Overwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")

	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	data := []byte("new content")
	if err := atomicWriteFile(path, data, 0o644); err != nil {
		t.Fatalf("atomicWriteFile: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("content = %q, want %q", got, data)
	}
}

func TestAtomicWriteFile_NoTempLeftover(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")

	if err := atomicWriteFile(path, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != "test.json" {
			t.Errorf("unexpected leftover file: %s", e.Name())
		}
	}
}

func TestAtomicWriteFile_Concurrent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			data := []byte(fmt.Sprintf(`{"n": %d}`, n))
			if err := atomicWriteFile(path, data, 0o644); err != nil {
				t.Errorf("goroutine %d: %v", n, err)
			}
		}(i)
	}
	wg.Wait()

	// File should exist and be valid JSON (one of the writes won).
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) == 0 {
		t.Error("file is empty after concurrent writes")
	}
}

func TestAtomicWriteFile_BadDir(t *testing.T) {
	// atomicWriteFile uses @inco: guards for error handling.
	// With inco test (overlay active), the guards return an error.
	err := atomicWriteFile("/nonexistent/dir/file.json", []byte("x"), 0o644)
	if err == nil {
		t.Error("expected error for nonexistent directory")
	}
}

func TestCleanTempFiles(t *testing.T) {
	dir := t.TempDir()

	// Create some temp files and a normal file.
	for _, name := range []string{".inco-tmp-abc", ".inco-tmp-def", "overlay.json"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	cleanTempFiles(dir)

	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != "overlay.json" {
			t.Errorf("unexpected file remaining: %s", e.Name())
		}
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 file, got %d", len(entries))
	}
}
