// Package ansiblebin locates the ansible-vault executable to run.
//
// This repo's own convention (see ansible-playbooks' venv-based setup)
// is a project-local Python venv at the repo root, not a global
// install -- so a plain PATH lookup fails whenever the venv hasn't been
// activated in the current shell, which is the common case for a
// wizard invoked fresh from wherever the operator happens to be. Resolve
// falls back to searching upward from a given anchor path (typically the
// vault.yml being operated on) and the current working directory for a
// conventional venv/bin/ansible-vault or .venv/bin/ansible-vault before
// giving up and returning the bare command name, which produces the
// same "not found in $PATH" error a plain exec.Command("ansible-vault",
// ...) would.
package ansiblebin

import (
	"os"
	"os/exec"
	"path/filepath"
)

// maxAncestors bounds the upward search so a deeply nested anchor path
// (or a weird cwd) can't turn this into an unbounded filesystem walk.
const maxAncestors = 16

// Resolve returns the ansible-vault executable to run: on $PATH if
// present there (an activated venv or a global install), otherwise
// $VIRTUAL_ENV/bin/ansible-vault if that env var is set, otherwise a
// venv/bin/ansible-vault or .venv/bin/ansible-vault found by walking up
// from anchor's directory (or, if anchor is empty or nothing's found
// there, from the current working directory). anchor is typically the
// vault.yml path the caller is about to operate on.
func Resolve(anchor string) string {
	if p, err := exec.LookPath("ansible-vault"); err == nil {
		return p
	}
	if ve := os.Getenv("VIRTUAL_ENV"); ve != "" {
		if p := venvCandidate(ve); p != "" {
			return p
		}
	}
	if anchor != "" {
		if p := searchUpward(filepath.Dir(anchor)); p != "" {
			return p
		}
	}
	if wd, err := os.Getwd(); err == nil {
		if p := searchUpward(wd); p != "" {
			return p
		}
	}
	return "ansible-vault"
}

func venvCandidate(venvRoot string) string {
	p := filepath.Join(venvRoot, "bin", "ansible-vault")
	if isExecutableFile(p) {
		return p
	}
	return ""
}

func searchUpward(start string) string {
	dir := start
	for range maxAncestors {
		for _, venvDir := range []string{"venv", ".venv"} {
			if p := filepath.Join(dir, venvDir, "bin", "ansible-vault"); isExecutableFile(p) {
				return p
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode()&0o111 != 0
}
