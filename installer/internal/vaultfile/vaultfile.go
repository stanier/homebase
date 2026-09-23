// Package vaultfile loads a vault.yml (plaintext scaffold or
// ansible-vault-encrypted), merges in new/changed key-value entries while
// preserving existing key order and formatting, and commits the result
// back -- either as a dry-run render or a real write.
//
// The vault password itself never passes through this package's own
// code: reads shell out to `ansible-vault view` and writes to an
// existing encrypted file shell out to `ansible-vault edit` with a
// non-interactive EDITOR shim, so in both cases Ansible's own prompt (or
// ANSIBLE_VAULT_PASSWORD_FILE/VAULT_PASS, which it already honors) is
// what actually supplies the password.
//
// Load and Commit run their subprocess directly and are what a plain
// CLI wants. A caller that owns the terminal itself -- a Bubble Tea
// program, which needs to pause its own raw-mode input handling before
// any subprocess can safely read the interactive password prompt --
// instead wants the two-phase PrepareLoad/(*LoadPlan).Finish and
// (*Vault).Prepare/(*CommitPlan).Done forms below, which hand back the
// unstarted *exec.Cmd so the caller can run it itself (e.g. via
// tea.ExecProcess) instead of vaultfile running it internally.
package vaultfile

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"homebase/installer/internal/ansiblebin"
)

const ansibleVaultHeader = "$ANSIBLE_VAULT;"

// Vault is an in-memory, order-preserving view of one vault.yml, plus
// the changes queued against it via Set.
type Vault struct {
	Path         string
	Existed      bool // false if the file didn't exist on disk when Loaded
	WasEncrypted bool // true if the on-disk file was ansible-vault encrypted

	doc  *yaml.Node // document root (Kind == DocumentNode); nil until first Set if the file was empty/missing
	map_ *yaml.Node // the top-level mapping node inside doc

	added   []string
	updated []string
}

// LoadPlan is PrepareLoad's result: everything needed to finish loading
// a vault.yml, plus (if the file is ansible-vault encrypted) the
// still-unstarted decrypt command the caller must Run() itself before
// calling Finish.
type LoadPlan struct {
	path         string
	existed      bool
	wasEncrypted bool
	plainData    []byte    // set when the file didn't need decrypting
	cmd          *exec.Cmd // set only when wasEncrypted; Cmd's Stdout is stdout
	stdout       *bytes.Buffer
}

// Cmd returns the decrypt command to run, or nil if there's nothing to
// run (the file is missing or was already plaintext) -- in which case
// Finish can be called immediately. When non-nil, the caller owns
// running it: set Stdin/Stderr (or leave them for the caller's own
// terminal-handoff mechanism, e.g. tea.ExecProcess) and call Run()
// before calling Finish.
func (lp *LoadPlan) Cmd() *exec.Cmd { return lp.cmd }

// PrepareLoad reads path and, if it's ansible-vault encrypted, builds
// (but does not run) the `ansible-vault view` command needed to decrypt
// it. A missing file is not an error -- LoadPlan.Cmd() is nil and
// Finish yields an empty, !Existed Vault.
func PrepareLoad(path string) (*LoadPlan, error) {
	lp := &LoadPlan{path: path}

	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return lp, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	lp.existed = true

	if bytes.HasPrefix(bytes.TrimLeft(raw, "\r\n \t"), []byte(ansibleVaultHeader)) {
		lp.wasEncrypted = true
		lp.cmd = exec.Command(ansiblebin.Resolve(path), "view", path)
		lp.stdout = &bytes.Buffer{}
		lp.cmd.Stdout = lp.stdout
		return lp, nil
	}

	lp.plainData = raw
	return lp, nil
}

// Finish parses whatever content PrepareLoad (plus, if Cmd() was
// non-nil, a completed run of it) produced into a Vault. Call it after
// Cmd() is nil, or after successfully running the *exec.Cmd it returned.
func (lp *LoadPlan) Finish() (*Vault, error) {
	v := &Vault{Path: lp.path, Existed: lp.existed, WasEncrypted: lp.wasEncrypted}

	data := lp.plainData
	if lp.wasEncrypted {
		data = lp.stdout.Bytes()
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return v, nil
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", lp.path, err)
	}
	if len(doc.Content) == 0 {
		return v, nil
	}
	m := doc.Content[0]
	if m.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s: expected a top-level YAML mapping, got %v", lp.path, m.Kind)
	}
	v.doc = &doc
	v.map_ = m
	return v, nil
}

// Load reads path, decrypting via `ansible-vault view` if it's
// encrypted, and runs that subprocess itself -- for a plain CLI that
// owns the terminal normally. A caller that needs to manage the
// terminal handoff itself (a Bubble Tea program) should use
// PrepareLoad/Finish instead.
func Load(path string) (*Vault, error) {
	lp, err := PrepareLoad(path)
	if err != nil {
		return nil, err
	}
	if cmd := lp.Cmd(); cmd != nil {
		cmd.Stdin = os.Stdin
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("ansible-vault view %s: %w", path, err)
		}
	}
	return lp.Finish()
}

// Has reports whether key is already defined (before any Set calls in
// this session).
func (v *Vault) Has(key string) bool {
	return v.findValueNode(key) != nil
}

// Get returns key's current string value, if defined. Satisfies the
// dependency-resolution shape internal/secretops needs (a plaintext
// value already staged for one vault_ key, to feed into generating
// another).
func (v *Vault) Get(key string) (string, bool) {
	n := v.findValueNode(key)
	if n == nil {
		return "", false
	}
	return n.Value, true
}

func (v *Vault) findValueNode(key string) *yaml.Node {
	if v.map_ == nil {
		return nil
	}
	for i := 0; i+1 < len(v.map_.Content); i += 2 {
		if v.map_.Content[i].Value == key {
			return v.map_.Content[i+1]
		}
	}
	return nil
}

// Set adds key if it isn't already defined, or updates its value if it
// is and the value actually differs. Either way it's recorded for the
// pending-changes summary (Added/Updated).
func (v *Vault) Set(key, value string) {
	if v.doc == nil {
		v.doc = &yaml.Node{
			Kind:    yaml.DocumentNode,
			Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}},
		}
		v.map_ = v.doc.Content[0]
	}

	style := yaml.DoubleQuotedStyle
	if strings.Contains(value, "\n") {
		style = yaml.LiteralStyle
	}

	if existing := v.findValueNode(key); existing != nil {
		if existing.Value == value {
			return
		}
		existing.Value = value
		existing.Style = style
		existing.Tag = "!!str"
		v.updated = append(v.updated, key)
		return
	}

	v.map_.Content = append(v.map_.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value, Style: style},
	)
	v.added = append(v.added, key)
}

// Added returns the keys newly introduced by Set calls this session.
func (v *Vault) Added() []string { return v.added }

// Updated returns the keys whose value Set calls this session actually
// changed (as opposed to setting it to the value it already had).
func (v *Vault) Updated() []string { return v.updated }

// Dirty reports whether any Set call actually changed anything.
func (v *Vault) Dirty() bool { return len(v.added)+len(v.updated) > 0 }

// Render returns the merged content as plaintext YAML, e.g. for a
// --dry-run preview. It never touches the filesystem.
func (v *Vault) Render() (string, error) {
	if v.doc == nil {
		return "", nil
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v.doc); err != nil {
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// CommitPlan is (*Vault).Prepare's result. If Cmd is nil, the write is
// already fully done -- see Prepare's doc for which case that is. Call
// Done after running Cmd (if it's non-nil) to clean up temp files.
type CommitPlan struct {
	Cmd     *exec.Cmd // nil if there's nothing left to run
	cleanup func()
}

// Done releases any temp files the plan created. Safe to call whether
// or not Cmd was run, and safe to call multiple times.
func (p *CommitPlan) Done() {
	if p.cleanup != nil {
		p.cleanup()
		p.cleanup = nil
	}
}

// Prepare stages a commit of the merged content back to v.Path, doing
// everything that doesn't require an interactive ansible-vault password
// prompt, and returns a plan for the rest:
//
//   - a brand-new file (!Existed) needs `ansible-vault encrypt`, which
//     prompts for a new vault password -- returned as Cmd;
//   - an existing plaintext scaffold file (not yet encrypted -- see
//     VAULT.md's first-time-setup step) is fully written during
//     Prepare itself (a plain file copy needs no subprocess) --
//     Cmd is nil;
//   - an existing encrypted file needs `ansible-vault edit` with a
//     non-interactive EDITOR shim, so it's re-encrypted with its
//     *existing* password without this package ever seeing it --
//     returned as Cmd.
//
// A pre-existing file is always backed up to Path+".bak" first. The
// caller must call the returned plan's Done() after running Cmd (if
// non-nil) to clean up temp files -- regardless of whether Cmd
// succeeded.
func (v *Vault) Prepare() (*CommitPlan, error) {
	if v.doc == nil {
		return nil, fmt.Errorf("nothing to commit")
	}
	rendered, err := v.Render()
	if err != nil {
		return nil, err
	}

	tmp, err := os.CreateTemp("", "vaultfile-merged-*.yml")
	if err != nil {
		return nil, err
	}
	tmpPath := tmp.Name()
	cleanupMerged := func() { os.Remove(tmpPath) }
	if _, err := tmp.WriteString(rendered); err != nil {
		tmp.Close()
		cleanupMerged()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		cleanupMerged()
		return nil, err
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		cleanupMerged()
		return nil, err
	}

	if !v.Existed {
		if err := os.MkdirAll(filepath.Dir(v.Path), 0o755); err != nil {
			cleanupMerged()
			return nil, err
		}
		cmd := exec.Command(ansiblebin.Resolve(v.Path), "encrypt", "--output", v.Path, tmpPath)
		return &CommitPlan{Cmd: cmd, cleanup: cleanupMerged}, nil
	}

	if err := backup(v.Path); err != nil {
		cleanupMerged()
		return nil, err
	}

	if !v.WasEncrypted {
		defer cleanupMerged()
		if err := copyFile(tmpPath, v.Path, 0o600); err != nil {
			return nil, err
		}
		return &CommitPlan{}, nil
	}

	cmd, cleanupShim, err := editInPlaceCommand(v.Path, tmpPath)
	if err != nil {
		cleanupMerged()
		return nil, err
	}
	return &CommitPlan{Cmd: cmd, cleanup: func() {
		cleanupMerged()
		cleanupShim()
	}}, nil
}

// Commit writes the merged content back to Path, running whatever
// subprocess Prepare's plan needs itself -- for a plain CLI that owns
// the terminal normally. A caller that needs to manage the terminal
// handoff itself (a Bubble Tea program) should use Prepare directly and
// run CommitPlan.Cmd (if non-nil) through its own mechanism instead.
func (v *Vault) Commit() error {
	plan, err := v.Prepare()
	if err != nil {
		return err
	}
	defer plan.Done()

	if plan.Cmd == nil {
		return nil
	}
	plan.Cmd.Stdin = os.Stdin
	plan.Cmd.Stdout = os.Stdout
	plan.Cmd.Stderr = os.Stderr
	if err := plan.Cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", strings.Join(plan.Cmd.Args, " "), err)
	}
	return nil
}

func backup(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %s for backup: %w", path, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path+".bak", data, info.Mode()); err != nil {
		return fmt.Errorf("writing backup %s.bak: %w", path, err)
	}
	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, mode)
}

// editInPlaceCommand builds (but doesn't run) the `ansible-vault edit`
// invocation that re-encrypts destPath with mergedPath's content,
// preserving destPath's existing vault password. `ansible-vault edit`
// decrypts to a temp file, invokes $EDITOR on it, and re-encrypts with
// the same password on exit -- pointing EDITOR at a throwaway shim
// script that just overwrites that temp file with our already-computed
// merged content turns this into a non-interactive commit without this
// process ever holding the vault password itself. The returned cleanup
// func removes the shim script and must be called once the command has
// run (or failed to).
func editInPlaceCommand(destPath, mergedPath string) (cmd *exec.Cmd, cleanup func(), err error) {
	shim, err := os.CreateTemp("", "vaultfile-editor-*.sh")
	if err != nil {
		return nil, nil, err
	}
	shimPath := shim.Name()
	cleanup = func() { os.Remove(shimPath) }

	script := fmt.Sprintf("#!/bin/sh\nset -eu\ncp %q \"$1\"\n", mergedPath)
	if _, err := shim.WriteString(script); err != nil {
		shim.Close()
		cleanup()
		return nil, nil, err
	}
	if err := shim.Close(); err != nil {
		cleanup()
		return nil, nil, err
	}
	if err := os.Chmod(shimPath, 0o700); err != nil {
		cleanup()
		return nil, nil, err
	}

	cmd = exec.Command(ansiblebin.Resolve(destPath), "edit", destPath)
	cmd.Env = append(os.Environ(), "EDITOR="+shimPath)
	return cmd, cleanup, nil
}
