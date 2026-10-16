package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// DefaultDir is writable by the current user on Windows, macOS and Linux.
func DefaultDir() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("locate user cache directory: %w", err)
	}
	return filepath.Join(dir, "isup"), nil
}

// Open creates a daily log file and removes only this application's dated logs
// older than retention. Daily files bound disk use without renaming open files.
func Open(dir string, retention int, now time.Time) (*os.File, error) {
	if retention < 1 {
		return nil, fmt.Errorf("log retention must be positive")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	cutoff := now.AddDate(0, 0, -retention).Format("2006-01-02")
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || len(name) != len("isup-2006-01-02.log") {
			continue
		}
		date, err := time.Parse("isup-2006-01-02.log", name)
		if err == nil && date.Format("2006-01-02") <= cutoff {
			if err := os.Remove(filepath.Join(dir, name)); err != nil {
				return nil, fmt.Errorf("remove expired log: %w", err)
			}
		}
	}
	return os.OpenFile(filepath.Join(dir, "isup-"+now.Format("2006-01-02")+".log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
}
