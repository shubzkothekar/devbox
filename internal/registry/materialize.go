// Package registry materializes a DevBox plugin registry — a Git
// repository pinned to a specific commit, or a local development
// override — into a local directory that internal/plugins can resolve
// manifests from.
package registry

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"

	"github.com/shubzkothekar/devbox/internal/config"
)

// Mode selects the lifecycle action Materialize performs.
type Mode uint8

const (
	// ReadOnly requires an existing, locked registry snapshot and
	// verifies it matches the lock file without any network access.
	ReadOnly Mode = iota
	// Install clones the configured registry fresh and materializes a
	// snapshot at the resolved commit, without requiring a pre-existing
	// lock.
	Install
	// Update re-resolves the configured registry ref to its latest
	// commit and replaces the materialized snapshot.
	Update
)

// Source describes a materialized registry: the local directory plugin
// manifests can be read from, and whether it came from a pinned Git
// snapshot or an unpinned local path override.
type Source struct {
	Root string
	Mode string // "git" or "path"
}

const generatedRegistryDir = ".generated/registry"

var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

// Materialize resolves state's configured registry (or local override)
// into a Source ready for plugin resolution.
//
// If state.Override is set, it always wins: the configured local path is
// canonicalized and returned as a Source with Mode "path", without
// reading or writing any lock file.
//
// Otherwise, mode controls how the pinned Git registry is materialized:
//   - ReadOnly requires a valid lock file and an existing
//     .generated/registry snapshot whose HEAD commit matches the lock.
//   - Install and Update clone state.Config.Registry.URL at
//     state.Config.Registry.Ref into a temporary directory, resolve its
//     HEAD commit, and atomically replace .generated/registry with that
//     clone. The caller is responsible for writing the lock file once
//     full catalog validation succeeds.
func Materialize(ctx context.Context, state config.ProjectState, mode Mode) (Source, error) {
	if state.Override != nil {
		return materializeLocalOverride(state.Override)
	}

	switch mode {
	case ReadOnly:
		return materializeReadOnly(ctx, state)
	case Install, Update:
		return materializeFromGit(ctx, state)
	default:
		return Source{}, fmt.Errorf("materialize registry: unknown mode %d", mode)
	}
}

func materializeLocalOverride(override *config.LocalOverride) (Source, error) {
	path := override.Registry.Path
	if path == "" {
		return Source{}, fmt.Errorf("materialize registry: local override registry.path must not be empty")
	}

	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return Source{}, fmt.Errorf("materialize registry: resolve local override path %q: %w", path, err)
	}

	info, err := os.Stat(resolved)
	if err != nil {
		return Source{}, fmt.Errorf("materialize registry: stat local override path %q: %w", resolved, err)
	}
	if !info.IsDir() {
		return Source{}, fmt.Errorf("materialize registry: local override path %q is not a directory", resolved)
	}

	return Source{Root: resolved, Mode: "path"}, nil
}

func materializeReadOnly(ctx context.Context, state config.ProjectState) (Source, error) {
	if state.Lock == nil {
		return Source{}, fmt.Errorf("materialize registry: no lock file found; run %q or %q first", "devbox plugin install", "devbox plugin update")
	}

	snapshotRoot := filepath.Join(state.Root, generatedRegistryDir)
	gitDir := filepath.Join(snapshotRoot, ".git")
	if info, err := os.Stat(gitDir); err != nil || !info.IsDir() {
		return Source{}, fmt.Errorf("materialize registry: no registry snapshot found at %s; run %q or %q first", snapshotRoot, "devbox plugin install", "devbox plugin update")
	}

	head, err := gitRevParseHEAD(ctx, snapshotRoot)
	if err != nil {
		return Source{}, fmt.Errorf("materialize registry: read snapshot HEAD: %w", err)
	}

	if head != state.Lock.Registry.Commit {
		return Source{}, fmt.Errorf("materialize registry: snapshot commit %s does not match locked commit %s; run %q to reconcile", head, state.Lock.Registry.Commit, "devbox plugin update")
	}

	return Source{Root: snapshotRoot, Mode: "git"}, nil
}

func materializeFromGit(ctx context.Context, state config.ProjectState) (Source, error) {
	url := state.Config.Registry.URL
	ref := state.Config.Registry.Ref
	if url == "" || ref == "" {
		return Source{}, fmt.Errorf("materialize registry: registry.url and registry.ref must not be empty")
	}

	tmpDir, err := os.MkdirTemp("", "devbox-registry-*")
	if err != nil {
		return Source{}, fmt.Errorf("materialize registry: create temporary clone directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	clonePath := filepath.Join(tmpDir, "clone")
	if err := runGit(ctx, "", "clone", "--depth", "1", "--branch", ref, "--", url, clonePath); err != nil {
		return Source{}, fmt.Errorf("materialize registry: clone %s: %w", scrubURL(url), err)
	}

	head, err := gitRevParseHEAD(ctx, clonePath)
	if err != nil {
		return Source{}, fmt.Errorf("materialize registry: resolve cloned HEAD: %w", err)
	}
	if !commitPattern.MatchString(head) {
		return Source{}, fmt.Errorf("materialize registry: resolved commit %q is not a valid 40-character git commit hash", head)
	}

	snapshotRoot := filepath.Join(state.Root, generatedRegistryDir)
	if err := replaceDirAtomically(snapshotRoot, clonePath); err != nil {
		return Source{}, fmt.Errorf("materialize registry: install snapshot: %w", err)
	}

	return Source{Root: snapshotRoot, Mode: "git"}, nil
}

// replaceDirAtomically atomically replaces dst with the contents of src by
// renaming src's parent-local staging copy into place. Because os.Rename
// cannot cross filesystem boundaries in general, src and dst's parent must
// reside on the same filesystem; the caller stages the clone under a
// temporary directory beside dst's parent when this matters. Here we
// perform a same-directory swap: rename any existing dst out of the way,
// rename src into dst's location, then remove the old directory.
func replaceDirAtomically(dst, src string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("create parent directory for %s: %w", dst, err)
	}

	staged := dst + ".new"
	if err := os.RemoveAll(staged); err != nil {
		return fmt.Errorf("clear staging directory %s: %w", staged, err)
	}
	if err := copyDir(src, staged); err != nil {
		os.RemoveAll(staged)
		return fmt.Errorf("stage snapshot at %s: %w", staged, err)
	}

	old := dst + ".old"
	os.RemoveAll(old)

	if _, err := os.Stat(dst); err == nil {
		if err := os.Rename(dst, old); err != nil {
			os.RemoveAll(staged)
			return fmt.Errorf("move existing snapshot at %s aside: %w", dst, err)
		}
	}

	if err := os.Rename(staged, dst); err != nil {
		os.RemoveAll(staged)
		if _, statErr := os.Stat(old); statErr == nil {
			os.Rename(old, dst)
		}
		return fmt.Errorf("install staged snapshot to %s: %w", dst, err)
	}

	os.RemoveAll(old)
	return nil
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
}

func gitRevParseHEAD(ctx context.Context, repoDir string) (string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "git", "-C", repoDir, "rev-parse", "HEAD")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git rev-parse HEAD: %w: %s", err, stderr.String())
	}
	sha := bytes.TrimSpace(stdout.Bytes())
	return string(sha), nil
}

// SnapshotCommit returns the HEAD commit hash of the git repository at repoDir.
func SnapshotCommit(ctx context.Context, repoDir string) (string, error) {
	return gitRevParseHEAD(ctx, repoDir)
}

func runGit(ctx context.Context, dir string, args ...string) error {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %v: %w: %s", scrubArgs(args), err, stderr.String())
	}
	return nil
}

// scrubArgs redacts URL-shaped arguments so credentials embedded in a
// registry URL (e.g. https://user:token@host/repo.git) never leak into
// error messages.
func scrubArgs(args []string) []string {
	scrubbed := make([]string, len(args))
	for i, a := range args {
		scrubbed[i] = scrubURL(a)
	}
	return scrubbed
}

// scrubURL redacts userinfo credentials embedded in a URL-shaped string.
func scrubURL(s string) string {
	return urlUserinfoPattern.ReplaceAllString(s, "$1***@")
}

var urlUserinfoPattern = regexp.MustCompile(`(://)[^/@]+@`)
