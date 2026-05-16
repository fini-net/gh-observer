package github

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fini-net/gh-observer/internal/debug"
)

var (
	jjDetected   bool
	jjGitRootVal string
	jjOnce       sync.Once
)

func detectJJ() (bool, string) {
	jjOnce.Do(func() {
		gitRoot, found, err := findJJGitRoot()
		if err != nil {
			debug.Log("jj detection failed", "err", err)
			jjDetected = false
			jjGitRootVal = ""
			return
		}
		jjDetected = found
		if found {
			jjGitRootVal = gitRoot
			debug.Log("jj detected", "gitRoot", gitRoot)
		} else {
			debug.Log("no jj repository detected")
		}
	})
	return jjDetected, jjGitRootVal
}

func findJJGitRoot() (gitRoot string, found bool, err error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", false, fmt.Errorf("get working directory: %w", err)
	}

	jjDir, found := findDotJJ(wd)
	if !found {
		return "", false, nil
	}

	debug.Log("found .jj directory", "path", jjDir)

	root, err := exec.Command("jj", "git", "root").Output()
	if err != nil {
		return "", false, fmt.Errorf("jj git root failed (is jj on PATH?): %w", err)
	}

	gitRoot = strings.TrimSpace(string(root))
	if gitRoot == "" {
		return "", false, fmt.Errorf("jj git root returned empty path")
	}

	return gitRoot, true, nil
}

func findDotJJ(dir string) (string, bool) {
	for {
		candidate := filepath.Join(dir, ".jj")
		info, err := os.Stat(candidate)
		if err == nil && info.IsDir() {
			return candidate, true
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func SetGITDirForJJ(cmd *exec.Cmd) {
	detected, gitRoot := detectJJ()
	if !detected || gitRoot == "" {
		return
	}

	if cmd.Env == nil {
		cmd.Env = os.Environ()
	}
	filtered := make([]string, 0, len(cmd.Env))
	for _, e := range cmd.Env {
		if !strings.HasPrefix(e, "GIT_DIR=") {
			filtered = append(filtered, e)
		}
	}
	filtered = append(filtered, fmt.Sprintf("GIT_DIR=%s", gitRoot))
	cmd.Env = filtered
	debug.Log("set GIT_DIR for jj compatibility", "GIT_DIR", gitRoot)
}

func IsJujutsu() bool {
	detected, _ := detectJJ()
	return detected
}
