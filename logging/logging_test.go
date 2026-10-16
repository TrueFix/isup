package logging

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenRetentionAndAppend(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "logs")
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	file, err := Open(dir, 30, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("first entry\n"); err != nil {
		t.Fatal(err)
	}
	file.Close()
	for _, name := range []string{"isup-2026-09-16.log", "isup-2026-09-17.log", "notes.log", "isup-not-a-date.log"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("keep"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	file, err = Open(dir, 30, now)
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	if _, err := os.Stat(filepath.Join(dir, "isup-2026-09-16.log")); !os.IsNotExist(err) {
		t.Fatalf("expired log still exists: %v", err)
	}
	for _, name := range []string{"isup-2026-09-17.log", "notes.log", "isup-not-a-date.log"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("removed unrelated or recent file %s: %v", name, err)
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, "isup-2026-10-16.log"))
	if err != nil || string(data) != "first entry\n" {
		t.Fatalf("existing log was not preserved: %q, %v", data, err)
	}
}

func TestOpenErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := Open(dir, 0, time.Now()); err == nil {
		t.Error("accepted zero retention")
	}
	path := filepath.Join(dir, "file")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path, 30, time.Now()); err == nil {
		t.Error("accepted a file as a directory")
	}
}
