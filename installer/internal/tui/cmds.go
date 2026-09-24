package tui

import (
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"homebase/installer/internal/catalog"
	"homebase/installer/internal/scan"
	"homebase/installer/internal/secretops"
	"homebase/installer/internal/vaultfile"
)

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func absPath(path string) (string, error) {
	return filepath.Abs(path)
}

func spinnerTick(m Model) tea.Cmd {
	return m.spin.Tick
}

// resolveVaultPath finds root's vault.yml, or -- if none exists yet --
// the conventional location a new one belongs at (matches
// main.go's copy for the CLI; kept as its own small copy here rather
// than a shared export since it's three lines and pulling in a
// cross-package dependency for it isn't worth it).
func resolveVaultPath(root string) (string, error) {
	env := scan.Env{Root: root}
	files, err := env.VaultFiles()
	if err != nil {
		return "", err
	}
	switch len(files) {
	case 0:
		return filepath.Join(root, "group_vars", "all", "vault.yml"), nil
	case 1:
		return files[0], nil
	default:
		return filepath.Join(root, "group_vars", "all", "vault.yml"), nil // ambiguous case shown as an error by the scan step instead
	}
}

// --- messages ---

type scanResultMsg struct {
	root      string
	vaultPath string
	refs      []scan.Reference
	loadPlan  *vaultfile.LoadPlan
	err       error
}

type vaultDecryptedMsg struct {
	err error
}

type vaultLoadedMsg struct {
	vault *vaultfile.Vault
	err   error
}

type generateResultMsg struct {
	key    string
	value  string
	reveal *secretops.Reveal
	err    error
}

type commitPlanMsg struct {
	plan *vaultfile.CommitPlan
	err  error
}

type commitDoneMsg struct {
	err error
}

// --- commands ---

// startScan does every bit of pure, non-interactive work up front:
// finding the vault.yml (if any), scanning for vault_ references, and
// preparing (but not running) its decrypt command if it's encrypted.
func startScan(root string) tea.Cmd {
	return func() tea.Msg {
		env := scan.Env{Root: root}
		refs, err := env.ScanReferences()
		if err != nil {
			return scanResultMsg{err: err}
		}
		vaultPath, err := resolveVaultPath(root)
		if err != nil {
			return scanResultMsg{err: err}
		}
		plan, err := vaultfile.PrepareLoad(vaultPath)
		if err != nil {
			return scanResultMsg{err: err}
		}
		return scanResultMsg{root: root, vaultPath: vaultPath, refs: refs, loadPlan: plan}
	}
}

// finishLoad parses whatever PrepareLoad/a completed decrypt produced.
// Pure and fast -- safe to call directly inside an ExecProcess callback.
func finishLoad(plan *vaultfile.LoadPlan) tea.Msg {
	v, err := plan.Finish()
	return vaultLoadedMsg{vault: v, err: err}
}

// runGenerate performs spec's generation strategy. Pure Go for
// RandomBase64/Sha512Crypt; StrategyExternalCmd shells out (e.g. to
// podman) but needs no terminal interaction, so it runs as a plain
// (background-goroutine) tea.Cmd rather than through tea.ExecProcess.
func runGenerate(key string, spec catalog.SecretSpec, deps secretops.DependencyResolver) tea.Cmd {
	return func() tea.Msg {
		value, reveal, err := secretops.Generate(spec, deps)
		return generateResultMsg{key: key, value: value, reveal: reveal, err: err}
	}
}

// prepareCommit runs (*vaultfile.Vault).Prepare -- backing up and, for
// the plaintext-scaffold case, fully writing the file -- without
// needing a terminal.
func prepareCommit(v *vaultfile.Vault) tea.Cmd {
	return func() tea.Msg {
		plan, err := v.Prepare()
		return commitPlanMsg{plan: plan, err: err}
	}
}
