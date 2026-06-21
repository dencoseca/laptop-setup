package stages

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"time"
)

type safeWriteResult struct {
	Changed    bool
	BackupPath string
}

func writeFileSafely(fsys FileSystem, destination string, payload []byte, perm fs.FileMode) (safeWriteResult, error) {
	if strings.TrimSpace(destination) == "" {
		return safeWriteResult{}, errors.New("destination path is required")
	}
	if err := fsys.MkdirAll(filepath.Dir(destination), privateDirPerm); err != nil {
		return safeWriteResult{}, fmt.Errorf("create destination directory: %w", err)
	}

	writePerm := perm
	info, statErr := fsys.Stat(destination)
	if statErr == nil {
		current, err := fsys.ReadFile(destination)
		if err != nil {
			return safeWriteResult{}, fmt.Errorf("read existing destination: %w", err)
		}
		if string(current) == string(payload) {
			return safeWriteResult{Changed: false}, nil
		}
		writePerm = info.Mode().Perm()
	} else if !errors.Is(statErr, fs.ErrNotExist) {
		return safeWriteResult{}, fmt.Errorf("stat destination: %w", statErr)
	}

	timestamp := time.Now().UTC().Format("20060102T150405.000000000Z")
	var backupPath string
	if statErr == nil {
		backupPath = destination + ".backup." + timestamp
		if err := copyFileFS(fsys, destination, backupPath, info.Mode().Perm()); err != nil {
			return safeWriteResult{}, fmt.Errorf("create timestamped backup: %w", err)
		}
	}

	tempPath := filepath.Join(filepath.Dir(destination), "."+filepath.Base(destination)+".tmp."+timestamp)
	if err := fsys.WriteFile(tempPath, payload, writePerm); err != nil {
		return safeWriteResult{}, fmt.Errorf("write temporary file: %w", err)
	}
	if err := fsys.Rename(tempPath, destination); err != nil {
		_ = fsys.Remove(tempPath)
		return safeWriteResult{}, fmt.Errorf("replace destination atomically: %w", err)
	}

	return safeWriteResult{Changed: true, BackupPath: backupPath}, nil
}

func copyFromTemplates(execCtx ExecutionContext, sourceName, destination string) error {
	return execCtx.templateStore().Copy(sourceName, destination)
}

func copyFileFS(fsys FileSystem, sourcePath, destinationPath string, perm fs.FileMode) error {
	payload, err := fsys.ReadFile(sourcePath)
	if err != nil {
		return fmt.Errorf("read source file: %w", err)
	}
	if err = fsys.WriteFile(destinationPath, payload, perm); err != nil {
		return fmt.Errorf("write destination file: %w", err)
	}
	return nil
}
