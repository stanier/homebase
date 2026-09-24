package ansiblebin

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func writeFakeVault(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho fake\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// clearPath hides any real ansible-vault on $PATH for the duration of
// the test, so Resolve is forced through the fallback search -- these
// tests must work in CI whether or not ansible-vault happens to be
// installed on the runner.
func clearPath(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("PATH-clearing assumes a POSIX-shaped PATH")
	}
	t.Setenv("PATH", "/nonexistent-empty-bin")
}

func TestResolveFindsVenvAboveAnchor(t *testing.T) {
	clearPath(t)
	t.Setenv("VIRTUAL_ENV", "")

	root := t.TempDir()
	vaultVault := filepath.Join(root, "venv", "bin", "ansible-vault")
	writeFakeVault(t, vaultVault)

	anchor := filepath.Join(root, "ansible-playbooks", "inventory", "testzone", "group_vars", "all", "vault.yml")
	if err := os.MkdirAll(filepath.Dir(anchor), 0o755); err != nil {
		t.Fatal(err)
	}

	got := Resolve(anchor)
	if got != vaultVault {
		t.Errorf("Resolve(%q) = %q, want %q", anchor, got, vaultVault)
	}
}

func TestResolveFindsDotVenv(t *testing.T) {
	clearPath(t)
	t.Setenv("VIRTUAL_ENV", "")

	root := t.TempDir()
	want := filepath.Join(root, ".venv", "bin", "ansible-vault")
	writeFakeVault(t, want)

	anchor := filepath.Join(root, "deep", "nested", "dir", "vault.yml")
	if err := os.MkdirAll(filepath.Dir(anchor), 0o755); err != nil {
		t.Fatal(err)
	}

	got := Resolve(anchor)
	if got != want {
		t.Errorf("Resolve(%q) = %q, want %q", anchor, got, want)
	}
}

func TestResolveUsesVirtualEnv(t *testing.T) {
	clearPath(t)

	root := t.TempDir()
	want := filepath.Join(root, "bin", "ansible-vault")
	writeFakeVault(t, want)
	t.Setenv("VIRTUAL_ENV", root)

	// An anchor with no venv of its own nearby -- VIRTUAL_ENV should
	// still win over any upward search.
	anchor := filepath.Join(t.TempDir(), "vault.yml")

	got := Resolve(anchor)
	if got != want {
		t.Errorf("Resolve(%q) = %q, want %q (from VIRTUAL_ENV)", anchor, got, want)
	}
}

func TestResolveFallsBackToBareName(t *testing.T) {
	clearPath(t)
	t.Setenv("VIRTUAL_ENV", "")

	// Resolve also falls back to searching upward from the current
	// working directory, and this test process's real cwd (somewhere
	// under this repo checkout) legitimately has a venv/bin/ansible-vault
	// ancestor -- chdir to an isolated temp dir so that fallback has
	// nothing to find either, and the bare-name path is what's left.
	isolated := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(isolated); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	anchor := filepath.Join(isolated, "vault.yml") // no venv anywhere nearby
	got := Resolve(anchor)
	if got != "ansible-vault" {
		t.Errorf("Resolve(%q) = %q, want the bare fallback %q", anchor, got, "ansible-vault")
	}
}
