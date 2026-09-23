// Package scan finds vault_* references under an inventory environment
// directory and diffs them against whatever's actually defined in that
// environment's vault.yml. It works without any catalog knowledge -- an
// unrecognized vault_ name still shows up as a gap, just without a
// description or generation strategy attached.
package scan

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"homebase/installer/internal/ansiblebin"
)

// varRefPattern matches vault_ identifiers wherever they appear -- inside
// "{{ vault_x }}" Jinja references, "{{ vault_x | default(”) }}", etc.
// Deliberately not anchored to "{{ }}" so it also catches a bare
// "vault_x:" typo'd into the wrong file.
var varRefPattern = regexp.MustCompile(`\bvault_[a-zA-Z0-9_]+\b`)

// referencableExt is which files are worth scanning for vault_ refs.
// hosts.ini files are plain inventory data (hostnames/groups), never
// Jinja templates, so they're skipped.
var referencableExt = map[string]bool{
	".yml":  true,
	".yaml": true,
}

// Env is one scanned inventory environment (e.g. inventory/testzone).
type Env struct {
	Root string // e.g. /path/to/inventory/testzone
}

// Reference is one vault_ name and every file it was found referenced in.
type Reference struct {
	Name  string
	Files []string // relative to Env.Root, sorted, deduplicated
}

// ScanReferences walks the environment directory (excluding vault.yml
// files themselves, which *define* vault_ names rather than reference
// them) and collects every vault_ identifier found, along with which
// file(s) it came from.
func (e Env) ScanReferences() ([]Reference, error) {
	found := map[string]map[string]bool{} // name -> set of relative file paths

	err := filepath.WalkDir(e.Root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == "vault.yml" {
			return nil
		}
		if !referencableExt[filepath.Ext(d.Name())] {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}
		rel, err := filepath.Rel(e.Root, path)
		if err != nil {
			rel = path
		}
		for _, m := range varRefPattern.FindAllString(string(content), -1) {
			if found[m] == nil {
				found[m] = map[string]bool{}
			}
			found[m][rel] = true
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	refs := make([]Reference, 0, len(found))
	for name, fileSet := range found {
		files := make([]string, 0, len(fileSet))
		for f := range fileSet {
			files = append(files, f)
		}
		sort.Strings(files)
		refs = append(refs, Reference{Name: name, Files: files})
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Name < refs[j].Name })
	return refs, nil
}

// VaultFiles finds every group_vars/**/vault.yml under the environment
// (normally just group_vars/all/vault.yml, but this doesn't assume that's
// the only one).
func (e Env) VaultFiles() ([]string, error) {
	var out []string
	err := filepath.WalkDir(e.Root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && d.Name() == "vault.yml" {
			out = append(out, path)
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}

// ansibleVaultHeader is how ansible-vault marks an encrypted file's first
// line; anything else is either plaintext scaffolding or not yet created.
const ansibleVaultHeader = "$ANSIBLE_VAULT;"

// ReadVaultKeys returns every top-level key already defined in a vault.yml
// file. A missing file returns an empty set, not an error (a brand-new
// environment has no vault.yml yet). An encrypted file is decrypted via
// `ansible-vault view`, inheriting this process's stdin/stderr so Ansible
// can prompt for the vault password itself -- this package never handles
// the password directly.
func ReadVaultKeys(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	if bytes.HasPrefix(bytes.TrimLeft(data, "\r\n \t"), []byte(ansibleVaultHeader)) {
		data, err = ansibleVaultView(path)
		if err != nil {
			return nil, err
		}
	}

	keys, err := yamlTopLevelKeys(data)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return keys, nil
}

func ansibleVaultView(path string) ([]byte, error) {
	cmd := exec.Command(ansiblebin.Resolve(path), "view", path)
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ansible-vault view %s: %w", path, err)
	}
	return out.Bytes(), nil
}

func yamlTopLevelKeys(data []byte) (map[string]bool, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return map[string]bool{}, nil
	}
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return nil, err
	}
	keys := map[string]bool{}
	if len(node.Content) == 0 {
		return keys, nil
	}
	doc := node.Content[0]
	if doc.Kind != yaml.MappingNode {
		return keys, nil
	}
	for i := 0; i < len(doc.Content); i += 2 {
		keys[doc.Content[i].Value] = true
	}
	return keys, nil
}

// Diff is the result of comparing referenced vault_ names against what a
// vault.yml actually defines.
type Diff struct {
	Missing      []Reference // referenced somewhere, not defined in vault.yml
	Present      []Reference // referenced and already defined
	Unreferenced []string    // defined in vault.yml but never referenced anywhere
}

// DiffAgainst compares scanned references against a vault.yml's existing
// keys.
func DiffAgainst(refs []Reference, existing map[string]bool) Diff {
	var d Diff
	seen := map[string]bool{}
	for _, r := range refs {
		seen[r.Name] = true
		if existing[r.Name] {
			d.Present = append(d.Present, r)
		} else {
			d.Missing = append(d.Missing, r)
		}
	}
	for k := range existing {
		if !seen[k] {
			d.Unreferenced = append(d.Unreferenced, k)
		}
	}
	sort.Strings(d.Unreferenced)
	return d
}

// IsVaultVar is a small guard so callers scanning arbitrary identifiers
// (e.g. future non-vault_ prefixed catalog entries) can filter cleanly.
func IsVaultVar(name string) bool {
	return strings.HasPrefix(name, "vault_")
}
