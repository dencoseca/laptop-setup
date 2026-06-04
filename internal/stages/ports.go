package stages

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dencoseca/laptop-setup/internal/runner"
)

const (
	privateDirPerm  fs.FileMode = 0o700
	privateFilePerm fs.FileMode = 0o600

	defaultAppleSiliconBrewPath = "/opt/homebrew/bin/brew"
)

type FileSystem interface {
	MkdirAll(path string, perm fs.FileMode) error
	ReadFile(name string) ([]byte, error)
	WriteFile(name string, data []byte, perm fs.FileMode) error
	Stat(name string) (fs.FileInfo, error)
	AppendFile(name string, data []byte, perm fs.FileMode) error
	Rename(oldpath string, newpath string) error
	Remove(name string) error
}

type TemplateStore interface {
	LoadBrewEntries(name string) ([]BrewEntry, string, error)
	Read(name string) ([]byte, string, error)
	WriteGeneratedBrewfile(runDir string, sourcePath string, entries []BrewEntry) (string, error)
	Copy(name string, destination string) error
}

type PackageManager interface {
	HomebrewAvailable(context.Context) error
	BrewBundleSatisfied(context.Context, ExecutionContext, string) (bool, error)
	RunBrewBundle(context.Context, ExecutionContext, string) error
}

type OSFileSystem struct{}

func (OSFileSystem) MkdirAll(path string, perm fs.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (OSFileSystem) ReadFile(name string) ([]byte, error) {
	return os.ReadFile(name)
}

func (OSFileSystem) WriteFile(name string, data []byte, perm fs.FileMode) error {
	return os.WriteFile(name, data, perm)
}

func (OSFileSystem) Stat(name string) (fs.FileInfo, error) {
	return os.Stat(name)
}

func (OSFileSystem) AppendFile(name string, data []byte, perm fs.FileMode) error {
	file, err := os.OpenFile(name, os.O_CREATE|os.O_APPEND|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(data)
	return err
}

func (OSFileSystem) Rename(oldpath string, newpath string) error {
	return os.Rename(oldpath, newpath)
}

func (OSFileSystem) Remove(name string) error {
	return os.Remove(name)
}

type FilesystemTemplateStore struct {
	RepoRoot string
	FS       FileSystem
}

type FSTemplateStore struct {
	FS         fs.FS
	SourceName string
	FileSystem FileSystem
}

func (s FilesystemTemplateStore) LoadBrewEntries(name string) ([]BrewEntry, string, error) {
	path, err := s.templatePath(name)
	if err != nil {
		return nil, "", err
	}
	content, err := s.fileSystem().ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("read Brewfile: %w", err)
	}
	return parseBrewEntries(string(content)), path, nil
}

func (s FilesystemTemplateStore) Read(name string) ([]byte, string, error) {
	path, err := s.templatePath(name)
	if err != nil {
		return nil, "", err
	}
	content, err := s.fileSystem().ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("read template %s: %w", name, err)
	}
	return content, path, nil
}

func (s FilesystemTemplateStore) WriteGeneratedBrewfile(runDir string, sourcePath string, entries []BrewEntry) (string, error) {
	return writeGeneratedBrewfile(s.fileSystem(), runDir, sourcePath, entries)
}

func (s FilesystemTemplateStore) Copy(name string, destination string) error {
	sourcePath, err := s.templatePath(name)
	if err != nil {
		return err
	}
	fs := s.fileSystem()
	payload, err := fs.ReadFile(sourcePath)
	if err != nil {
		return fmt.Errorf("read source file: %w", err)
	}
	return copyTemplatePayload(fs, destination, payload)
}

func (s FilesystemTemplateStore) templatePath(name string) (string, error) {
	if strings.TrimSpace(s.RepoRoot) == "" {
		return "", errors.New("repository root is required")
	}
	return filepath.Join(s.RepoRoot, "templates", name), nil
}

func (s FilesystemTemplateStore) fileSystem() FileSystem {
	if s.FS != nil {
		return s.FS
	}
	return OSFileSystem{}
}

func (s FSTemplateStore) LoadBrewEntries(name string) ([]BrewEntry, string, error) {
	content, err := fs.ReadFile(s.templateFS(), name)
	if err != nil {
		return nil, "", fmt.Errorf("read Brewfile: %w", err)
	}
	return parseBrewEntries(string(content)), s.sourcePath(name), nil
}

func (s FSTemplateStore) Read(name string) ([]byte, string, error) {
	content, err := fs.ReadFile(s.templateFS(), name)
	if err != nil {
		return nil, "", fmt.Errorf("read template %s: %w", name, err)
	}
	return content, s.sourcePath(name), nil
}

func (s FSTemplateStore) WriteGeneratedBrewfile(runDir string, sourcePath string, entries []BrewEntry) (string, error) {
	return writeGeneratedBrewfile(s.fileSystem(), runDir, sourcePath, entries)
}

func (s FSTemplateStore) Copy(name string, destination string) error {
	payload, err := fs.ReadFile(s.templateFS(), name)
	if err != nil {
		return fmt.Errorf("read source file: %w", err)
	}
	fsys := s.fileSystem()
	return copyTemplatePayload(fsys, destination, payload)
}

func (s FSTemplateStore) templateFS() fs.FS {
	if s.FS != nil {
		return s.FS
	}
	return os.DirFS("templates")
}

func (s FSTemplateStore) sourcePath(name string) string {
	sourceName := strings.TrimSpace(s.SourceName)
	if sourceName == "" {
		sourceName = "embedded templates"
	}
	return sourceName + ":" + name
}

func (s FSTemplateStore) fileSystem() FileSystem {
	if s.FileSystem != nil {
		return s.FileSystem
	}
	return OSFileSystem{}
}

func writeGeneratedBrewfile(fsys FileSystem, runDir string, sourcePath string, entries []BrewEntry) (string, error) {
	if strings.TrimSpace(runDir) == "" {
		return "", errors.New("run directory is required to generate Brewfile")
	}
	if len(entries) == 0 {
		return "", errors.New("generated Brewfile would be empty")
	}

	var builder strings.Builder
	builder.WriteString("# Generated by laptop-setup. Do not edit.\n")
	builder.WriteString(fmt.Sprintf("# Source: %s\n\n", sourcePath))
	for _, entry := range entries {
		builder.WriteString(entry.Line)
		builder.WriteString("\n")
	}

	if err := fsys.MkdirAll(runDir, privateDirPerm); err != nil {
		return "", fmt.Errorf("create run directory: %w", err)
	}
	path := filepath.Join(runDir, "Brewfile.generated")
	if _, err := writeFileSafely(fsys, path, []byte(builder.String()), privateFilePerm); err != nil {
		return "", fmt.Errorf("write generated Brewfile: %w", err)
	}
	return path, nil
}

func copyTemplatePayload(fsys FileSystem, destination string, payload []byte) error {
	if _, err := writeFileSafely(fsys, destination, payload, privateFilePerm); err != nil {
		return fmt.Errorf("write destination file: %w", err)
	}
	return nil
}

type HomebrewPackageManager struct {
	Runner   runner.CommandRunner
	BrewPath string
}

func (m HomebrewPackageManager) HomebrewAvailable(ctx context.Context) error {
	_, err := m.ResolveBrewPath(ctx)
	return err
}

func (m HomebrewPackageManager) ResolveBrewPath(ctx context.Context) (string, error) {
	if m.Runner == nil {
		return "", errors.New("runner is required")
	}
	path, pathErr := m.Runner.LookPath(ctx, "brew")
	if pathErr == nil {
		return path, nil
	}

	brewPath := strings.TrimSpace(m.BrewPath)
	if brewPath == "" {
		brewPath = defaultAppleSiliconBrewPath
	}
	path, fallbackErr := m.Runner.LookPath(ctx, brewPath)
	if fallbackErr == nil {
		return path, nil
	}

	return "", fmt.Errorf("brew executable not found in PATH (%v) or at %s (%v)", pathErr, brewPath, fallbackErr)
}

func (m HomebrewPackageManager) RunBrewBundle(ctx context.Context, execCtx ExecutionContext, brewfilePath string) error {
	brewPath, err := m.ResolveBrewPath(ctx)
	if err != nil {
		return err
	}
	return runCommand(ctx, execCtx, runner.Command{
		Name:        brewPath,
		Args:        []string{"bundle", "install", "--file", brewfilePath},
		Interactive: true,
		Prompt:      "Homebrew may ask for administrator authorization while installing selected packages or apps.",
	})
}

func (m HomebrewPackageManager) BrewBundleSatisfied(ctx context.Context, execCtx ExecutionContext, brewfilePath string) (bool, error) {
	brewPath, err := m.ResolveBrewPath(ctx)
	if err != nil {
		return false, err
	}
	if execCtx.Runner == nil {
		return false, errors.New("runner is required")
	}
	result, err := execCtx.Runner.Run(ctx, runner.Command{
		Name: brewPath,
		Args: []string{"bundle", "check", "--file", brewfilePath},
	})
	if err == nil {
		return true, nil
	}
	if result.ExitCode >= 1 {
		return false, nil
	}
	return false, err
}

func ResolveSelectedBrewIDsFromStore(store TemplateStore) ([]string, error) {
	if store == nil {
		return nil, errors.New("template store is required")
	}
	entries, _, err := store.LoadBrewEntries(defaultBrewTemplate)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.ID)
	}
	return slices.Compact(ids), nil
}

func (execCtx ExecutionContext) fileSystem() FileSystem {
	if execCtx.FileSystem != nil {
		return execCtx.FileSystem
	}
	return OSFileSystem{}
}

func (execCtx ExecutionContext) templateStore() TemplateStore {
	if execCtx.TemplateStore != nil {
		return execCtx.TemplateStore
	}
	return FilesystemTemplateStore{
		RepoRoot: execCtx.RepoRoot,
		FS:       execCtx.fileSystem(),
	}
}

func (execCtx ExecutionContext) packageManager() PackageManager {
	if execCtx.PackageManager != nil {
		return execCtx.PackageManager
	}
	return HomebrewPackageManager{Runner: execCtx.Runner}
}
