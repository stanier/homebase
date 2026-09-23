package scan

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestScanFixture exercises the full path -- reference scanning, an
// ansible-vault-encrypted vault.yml, and the resulting diff -- against
// testdata/fixture-env. Requires ansible-vault on PATH; skipped if it's
// not available.
func TestScanFixture(t *testing.T) {
	if _, err := exec.LookPath("ansible-vault"); err != nil {
		t.Skip("ansible-vault not on PATH")
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(wd, "..", "..", "testdata", "fixture-env")
	passFile := filepath.Join(wd, "..", "..", "testdata", "fixture-vault-pass")

	t.Setenv("ANSIBLE_VAULT_PASSWORD_FILE", passFile)

	env := Env{Root: fixture}
	refs, err := env.ScanReferences()
	if err != nil {
		t.Fatalf("ScanReferences: %v", err)
	}
	wantRefs := map[string]bool{"vault_foo": true, "vault_bar": true, "vault_baz": true, "vault_qux": true}
	if len(refs) != len(wantRefs) {
		t.Fatalf("got %d refs, want %d: %+v", len(refs), len(wantRefs), refs)
	}
	for _, r := range refs {
		if !wantRefs[r.Name] {
			t.Errorf("unexpected reference %q", r.Name)
		}
	}

	vaultFiles, err := env.VaultFiles()
	if err != nil {
		t.Fatalf("VaultFiles: %v", err)
	}
	if len(vaultFiles) != 1 {
		t.Fatalf("got %d vault files, want 1: %v", len(vaultFiles), vaultFiles)
	}

	existing, err := ReadVaultKeys(vaultFiles[0])
	if err != nil {
		t.Fatalf("ReadVaultKeys: %v", err)
	}
	wantExisting := map[string]bool{"vault_foo": true, "vault_bar": true, "vault_stale": true}
	for k := range wantExisting {
		if !existing[k] {
			t.Errorf("expected existing key %q not found", k)
		}
	}

	d := DiffAgainst(refs, existing)

	gotMissing := map[string]bool{}
	for _, r := range d.Missing {
		gotMissing[r.Name] = true
	}
	wantMissing := map[string]bool{"vault_baz": true, "vault_qux": true}
	if len(gotMissing) != len(wantMissing) {
		t.Fatalf("missing = %v, want %v", gotMissing, wantMissing)
	}
	for k := range wantMissing {
		if !gotMissing[k] {
			t.Errorf("expected %q in Missing", k)
		}
	}

	gotPresent := map[string]bool{}
	for _, r := range d.Present {
		gotPresent[r.Name] = true
	}
	wantPresent := map[string]bool{"vault_foo": true, "vault_bar": true}
	if len(gotPresent) != len(wantPresent) {
		t.Fatalf("present = %v, want %v", gotPresent, wantPresent)
	}

	if len(d.Unreferenced) != 1 || d.Unreferenced[0] != "vault_stale" {
		t.Errorf("unreferenced = %v, want [vault_stale]", d.Unreferenced)
	}
}

func TestReadVaultKeysMissingFile(t *testing.T) {
	keys, err := ReadVaultKeys(filepath.Join(t.TempDir(), "does-not-exist.yml"))
	if err != nil {
		t.Fatalf("ReadVaultKeys on missing file: %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("expected empty key set, got %v", keys)
	}
}

func TestReadVaultKeysPlaintextScaffold(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.yml")
	if err := os.WriteFile(path, []byte("vault_a: x\nvault_b: y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	keys, err := ReadVaultKeys(path)
	if err != nil {
		t.Fatalf("ReadVaultKeys: %v", err)
	}
	if !keys["vault_a"] || !keys["vault_b"] || len(keys) != 2 {
		t.Errorf("got %v, want {vault_a, vault_b}", keys)
	}
}
