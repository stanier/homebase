package vaultfile

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func requireAnsibleVault(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ansible-vault"); err != nil {
		t.Skip("ansible-vault not on PATH")
	}
}

func TestLoadMissingFile(t *testing.T) {
	v, err := Load(filepath.Join(t.TempDir(), "vault.yml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if v.Existed {
		t.Error("Existed = true for a file that doesn't exist")
	}
	if v.Has("vault_x") {
		t.Error("Has(vault_x) = true on an empty vault")
	}
}

func TestSetAddAndNoOpUpdate(t *testing.T) {
	v, err := Load(filepath.Join(t.TempDir(), "vault.yml"))
	if err != nil {
		t.Fatal(err)
	}
	v.Set("vault_a", "one")
	if !v.Dirty() {
		t.Fatal("expected Dirty after Set on a new key")
	}
	if got, ok := v.Get("vault_a"); !ok || got != "one" {
		t.Fatalf("Get(vault_a) = %q, %v", got, ok)
	}
	if len(v.Added()) != 1 || v.Added()[0] != "vault_a" {
		t.Fatalf("Added() = %v", v.Added())
	}

	// Setting the same key to the same value again must not re-record it.
	v.Set("vault_a", "one")
	if len(v.Added()) != 1 || len(v.Updated()) != 0 {
		t.Fatalf("re-setting same value changed tracking: added=%v updated=%v", v.Added(), v.Updated())
	}

	// A genuinely different value is an update, not another add.
	v.Set("vault_a", "two")
	if len(v.Added()) != 1 || len(v.Updated()) != 1 {
		t.Fatalf("expected one add + one update, got added=%v updated=%v", v.Added(), v.Updated())
	}
	if got, _ := v.Get("vault_a"); got != "two" {
		t.Fatalf("Get(vault_a) after update = %q, want two", got)
	}
}

func TestRenderPreservesOrderAndQuotesStrings(t *testing.T) {
	v, err := Load(filepath.Join(t.TempDir(), "vault.yml"))
	if err != nil {
		t.Fatal(err)
	}
	v.Set("vault_z", "last-declared-first")
	v.Set("vault_a", "123") // numeric-looking value must stay a string
	v.Set("vault_multiline", "line1\nline2")

	rendered, err := v.Render()
	if err != nil {
		t.Fatal(err)
	}

	zIdx := strings.Index(rendered, "vault_z")
	aIdx := strings.Index(rendered, "vault_a")
	if zIdx < 0 || aIdx < 0 || zIdx > aIdx {
		t.Fatalf("expected vault_z before vault_a (insertion order), got:\n%s", rendered)
	}
	if !strings.Contains(rendered, `vault_a: "123"`) {
		t.Errorf("expected vault_a's numeric-looking value quoted as a string, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "vault_multiline: |") {
		t.Errorf("expected a multi-line value rendered as a literal block, got:\n%s", rendered)
	}
}

// TestCommitRoundTrip exercises the real ansible-vault integration: a
// brand-new vault.yml gets freshly encrypted, then a second Commit against
// the same file re-encrypts with its *existing* password (no new prompt)
// via the ansible-vault edit/EDITOR shim, preserving keys Set didn't touch.
func TestCommitRoundTrip(t *testing.T) {
	requireAnsibleVault(t)

	dir := t.TempDir()
	passFile := filepath.Join(dir, "pass")
	if err := os.WriteFile(passFile, []byte("round-trip-test-password"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANSIBLE_VAULT_PASSWORD_FILE", passFile)

	vaultPath := filepath.Join(dir, "group_vars", "all", "vault.yml")

	// First commit: brand-new file.
	v, err := Load(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	v.Set("vault_a", "one")
	if err := v.Commit(); err != nil {
		t.Fatalf("first Commit: %v", err)
	}
	if _, err := os.Stat(vaultPath + ".bak"); !os.IsNotExist(err) {
		t.Error("a brand-new file shouldn't produce a .bak")
	}

	// Second commit: existing encrypted file, add one key, leave the
	// first alone -- both should survive, still under the same password.
	v2, err := Load(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	if !v2.WasEncrypted {
		t.Fatal("expected the first commit's output to be encrypted")
	}
	if got, ok := v2.Get("vault_a"); !ok || got != "one" {
		t.Fatalf("Get(vault_a) after reload = %q, %v", got, ok)
	}
	v2.Set("vault_b", "two")
	if err := v2.Commit(); err != nil {
		t.Fatalf("second Commit: %v", err)
	}
	if _, err := os.Stat(vaultPath + ".bak"); err != nil {
		t.Errorf("expected a .bak from the second commit: %v", err)
	}

	v3, err := Load(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := v3.Get("vault_a"); !ok || got != "one" {
		t.Errorf("vault_a lost across re-encrypt: got %q, %v", got, ok)
	}
	if got, ok := v3.Get("vault_b"); !ok || got != "two" {
		t.Errorf("vault_b missing after second commit: got %q, %v", got, ok)
	}
}
