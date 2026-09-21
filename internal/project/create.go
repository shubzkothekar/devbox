package project

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// DefaultScaffoldURL is the default Git repository URL for the DevBox scaffold.
const DefaultScaffoldURL = "https://github.com/shubzkothekar/devbox.git"

var validNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// CreateRequest encapsulates parameters for bootstrapping a new DevBox project.
type CreateRequest struct {
	Name        string
	Destination string
	Ref         string
	ScaffoldURL string
}

// CreateResult holds metadata for the successfully created DevBox project.
type CreateResult struct {
	Root string
	Ref  string
}

// Create clones a scaffold at ref, initializes .env, and rejects unsafe destinations.
func Create(ctx context.Context, request CreateRequest) (CreateResult, error) {
	if request.Name == "" || filepath.Base(request.Name) != request.Name || !validNamePattern.MatchString(request.Name) {
		return CreateResult{}, fmt.Errorf("invalid project name %q: must match %s and have no path separators", request.Name, validNamePattern.String())
	}

	dest := request.Destination
	if dest == "" {
		dest = "./" + request.Name
	}

	if _, err := os.Lstat(dest); err == nil {
		return CreateResult{}, fmt.Errorf("destination %q already exists", dest)
	} else if !errors.Is(err, os.ErrNotExist) {
		return CreateResult{}, fmt.Errorf("stat destination: %w", err)
	}

	scaffoldURL := request.ScaffoldURL
	if scaffoldURL == "" {
		scaffoldURL = os.Getenv("DEVBOX_SCAFFOLD_URL")
	}
	if scaffoldURL == "" {
		scaffoldURL = DefaultScaffoldURL
	}

	var cloneArgs []string
	if request.Ref != "" {
		cloneArgs = []string{"clone", "--branch", request.Ref, "--single-branch", scaffoldURL, dest}
	} else {
		cloneArgs = []string{"clone", scaffoldURL, dest}
	}

	cloneCmd := exec.CommandContext(ctx, "git", cloneArgs...)
	var stderr bytes.Buffer
	cloneCmd.Stderr = &stderr
	if err := cloneCmd.Run(); err != nil {
		_ = os.RemoveAll(dest)
		return CreateResult{}, fmt.Errorf("clone scaffold: %v: %s", err, strings.TrimSpace(stderr.String()))
	}

	// Determine checked-out ref
	ref := request.Ref
	if ref == "" {
		refCmd := exec.CommandContext(ctx, "git", "-C", dest, "rev-parse", "--abbrev-ref", "HEAD")
		if out, err := refCmd.Output(); err == nil {
			ref = strings.TrimSpace(string(out))
		}
	}

	// Initialize .env from .env.example if absent
	examplePath := filepath.Join(dest, ".env.example")
	envPath := filepath.Join(dest, ".env")
	if _, err := os.Lstat(examplePath); err == nil {
		if _, err := os.Lstat(envPath); errors.Is(err, os.ErrNotExist) {
			if err := copyEnvExample(examplePath, envPath); err != nil {
				_ = os.RemoveAll(dest)
				return CreateResult{}, fmt.Errorf("initialize .env: %w", err)
			}
		}
	}

	return CreateResult{
		Root: dest,
		Ref:  ref,
	}, nil
}

func copyEnvExample(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
