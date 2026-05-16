package github

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestFindDotJJ(t *testing.T) {
	tmpDir := t.TempDir()

	_, found := findDotJJ(tmpDir)
	if found {
		t.Errorf("findDotJJ(%q) should not find .jj in empty dir", tmpDir)
	}

	jjDir := filepath.Join(tmpDir, ".jj")
	if err := os.Mkdir(jjDir, 0o755); err != nil {
		t.Fatalf("failed to create .jj dir: %v", err)
	}

	result, found := findDotJJ(tmpDir)
	if !found {
		t.Errorf("findDotJJ(%q) should find .jj dir", tmpDir)
	}
	if result != jjDir {
		t.Errorf("findDotJJ(%q) = %q, want %q", tmpDir, result, jjDir)
	}

	subDir := filepath.Join(tmpDir, "sub", "deep")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("failed to create subdirectory: %v", err)
	}

	result, found = findDotJJ(subDir)
	if !found {
		t.Errorf("findDotJJ(%q) should find .jj in parent %q", subDir, tmpDir)
	}
	if result != jjDir {
		t.Errorf("findDotJJ(%q) = %q, want %q", subDir, result, jjDir)
	}
}

func TestFindDotJJNotFound(t *testing.T) {
	tmpDir := t.TempDir()

	_, found := findDotJJ(tmpDir)
	if found {
		t.Errorf("findDotJJ(%q) should not find .jj when none exists", tmpDir)
	}
}

func TestSetGITDirForJJNoJJ(t *testing.T) {
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir to temp dir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(origDir); err != nil {
			t.Logf("restore cwd: %v", err)
		}
	})

	resetJJDetection()

	cmd := exec.Command("echo", "test")
	SetGITDirForJJ(cmd)

	found := false
	for _, env := range cmd.Env {
		if strings.HasPrefix(env, "GIT_DIR=") {
			found = true
		}
	}
	if found {
		t.Error("SetGITDirForJJ should not set GIT_DIR when jj not detected")
	}
}

func TestSetGITDirForJJReplacesExistingGITDir(t *testing.T) {
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir to temp dir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(origDir); err != nil {
			t.Logf("restore cwd: %v", err)
		}
	})

	resetJJDetection()

	jjOnce.Do(func() {
		jjDetected = true
		jjGitRootVal = "/fake/git/root"
	})

	cmd := exec.Command("echo", "test")
	env := filterGITDir(os.Environ())
	env = append(env, "GIT_DIR=/old/path")
	cmd.Env = env
	SetGITDirForJJ(cmd)

	gitDirCount := 0
	gitDirValue := ""
	for _, env := range cmd.Env {
		if strings.HasPrefix(env, "GIT_DIR=") {
			gitDirCount++
			gitDirValue = strings.TrimPrefix(env, "GIT_DIR=")
		}
	}
	if gitDirCount != 1 {
		t.Errorf("expected exactly 1 GIT_DIR entry, found %d", gitDirCount)
	}
	if gitDirValue != "/fake/git/root" {
		t.Errorf("GIT_DIR = %q, want %q", gitDirValue, "/fake/git/root")
	}
}

func filterGITDir(env []string) []string {
	filtered := make([]string, 0, len(env))
	for _, e := range env {
		if !strings.HasPrefix(e, "GIT_DIR=") {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

func resetJJDetection() {
	jjOnce = sync.Once{}
	jjDetected = false
	jjGitRootVal = ""
}
