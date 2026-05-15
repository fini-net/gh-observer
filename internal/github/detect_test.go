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
	resetJJDetection()

	cmd := exec.Command("echo", "test")
	cmd.Env = append(os.Environ(), "GIT_DIR=/old/path")
	SetGITDirForJJ(cmd)

	count := 0
	for _, env := range cmd.Env {
		if strings.HasPrefix(env, "GIT_DIR=") {
			count++
		}
	}
	if count > 1 {
		t.Errorf("SetGITDirForJJ should replace existing GIT_DIR, found %d entries", count)
	}
}

func resetJJDetection() {
	jjOnce = sync.Once{}
	jjDetected = false
	jjGitRootVal = ""
}
