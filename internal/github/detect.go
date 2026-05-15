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
		gitRoot, err := findJJGitRoot()
		if err != nil {
			debug.Log("jj detection failed", "err", err)
			jjDetected = false
			jjGitRootVal = ""
			return
		}
		jjDetected = true
		jjGitRootVal = gitRoot
		debug.Log("jj detected", "gitRoot", gitRoot)
	})
	return jjDetected, jjGitRootVal
}

func findJJGitRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}

 jjDir, found := findDotJJ(wd)
	if !found {
		return "", nil
	}

	debug.Log("found .jj directory", "path", jjDir)

	root, err := exec.Command("jj", "git", "root").Output()
	if err != nil {
		return "", fmt.Errorf("jj git root failed (is jj on PATH?): %w", err)
	}

	gitRoot := strings.TrimSpace(string(root))
	if gitRoot == "" {
		return "", fmt.Errorf("jj git root returned empty path")
	}

	return gitRoot, nil
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
	cmd.Env = append(cmd.Env, fmt.Sprintf("GIT_DIR=%s", gitRoot))
	debug.Log("set GIT_DIR for jj compatibility", "GIT_DIR", gitRoot)
}

func IsJujutsu() bool {
	detected, _ := detectJJ()
	return detected
}