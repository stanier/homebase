// Command installer helps generate/import the secrets an
// ansible-playbooks inventory environment needs, instead of hand-copying
// commands out of ansible-playbooks/docs/VAULT.md: `scan`/`set` for
// scripted use, `wizard` for a guided quickstart/update walkthrough over
// the same scan+set+generate foundation.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"homebase/installer/internal/catalog"
	"homebase/installer/internal/scan"
	"homebase/installer/internal/secretops"
	"homebase/installer/internal/tui"
	"homebase/installer/internal/vaultfile"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "scan":
		os.Exit(runScan(os.Args[2:]))
	case "set":
		os.Exit(runSet(os.Args[2:]))
	case "wizard":
		os.Exit(runWizard(os.Args[2:]))
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func runWizard(args []string) int {
	fs := flag.NewFlagSet("wizard", flag.ExitOnError)
	envDir := fs.String("i", "", "path to an inventory environment (optional -- can also be entered in the wizard)")
	fs.Parse(args)

	p := tea.NewProgram(tui.New(*envDir), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "wizard:", err)
		return 1
	}
	return 0
}

func usage() {
	fmt.Fprintln(os.Stderr, `installer -- secret bootstrap helper for ansible-playbooks inventories

Usage:
  installer wizard [-i <inventory-env-dir>]
  installer scan   -i <inventory-env-dir> [-v]
  installer set    -i <inventory-env-dir> [-dry-run] ARG [ARG ...]

  wizard Without -i, first asks whether to fill secrets for an existing
         environment or start a new one (a service picker over
         internal/svccatalog -- preview only for now, doesn't generate
         hosts.ini/host_vars yet). With -i, skips straight to the
         existing-environment flow: decrypt its vault.yml if it has
         one, then walk through every missing vault_* secret one at a
         time -- generate, paste, or skip each -- and commit at the
         end. Same scan/set/generate foundation as the commands below,
         just guided.
  scan   Report vault_* variables referenced under the environment
         directory but not yet defined in its vault.yml.
  set    Add or update one or more vault_* keys in that environment's
         vault.yml. Never overwrites a key with the value it already
         has (no-op), but WILL overwrite a key given a genuinely
         different value -- review the printed summary before
         confirming a real (non -dry-run) run.

         Each ARG is either:
           KEY=VALUE   set KEY to an explicit value
           KEY         generate KEY's value using its catalog entry's
                       strategy (fails if KEY has no catalog entry, or
                       its strategy is import-only -- those need an
                       explicit KEY=VALUE instead)

         A generated SHA512-CRYPT entry (e.g. vault_root_password_hash)
         also generates a random plaintext password to hash -- only the
         hash goes in vault.yml, so that plaintext is printed once and
         must be saved somewhere outside this tool (it's the actual
         login credential; there is no way to recover it afterward).

         An external-command entry (e.g. the Wazuh password hashes)
         needs its dependency's plaintext already resolved -- list that
         key earlier in the same command, or set/generate it in a prior
         run first.

Options for scan:
  -i    Path to an inventory environment, e.g. ansible-playbooks/inventory/testzone
  -v    Also list variables already present

Options for set:
  -i        Path to an inventory environment (required)
  -dry-run  Print the merged vault.yml instead of writing it. Generation
            still runs (a value has to be computed to preview it) --
            including any external command a strategy shells out to --
            only the vault.yml write itself is skipped, and a real run
            afterward generates a fresh (different) random value.`)
}

// resolveVaultPath finds the environment's vault.yml, or -- if none
// exists yet -- the conventional location a new one belongs at
// (group_vars/all/vault.yml, same layout every real environment uses).
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
		return "", fmt.Errorf("found %d vault.yml files under %s, expected at most 1: %s", len(files), root, strings.Join(files, ", "))
	}
}

func runScan(args []string) int {
	fs := flag.NewFlagSet("scan", flag.ExitOnError)
	envDir := fs.String("i", "", "path to an inventory environment (required)")
	verbose := fs.Bool("v", false, "also list variables already present in vault.yml")
	fs.Parse(args)

	if *envDir == "" {
		fmt.Fprintln(os.Stderr, "scan: -i <inventory-env-dir> is required")
		return 2
	}
	root, err := filepath.Abs(*envDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "scan:", err)
		return 1
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		fmt.Fprintf(os.Stderr, "scan: %s is not a directory\n", root)
		return 1
	}

	env := scan.Env{Root: root}
	refs, err := env.ScanReferences()
	if err != nil {
		fmt.Fprintln(os.Stderr, "scan:", err)
		return 1
	}

	vaultPath, err := resolveVaultPath(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "scan:", err)
		return 1
	}
	existing, err := scan.ReadVaultKeys(vaultPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "scan:", err)
		return 1
	}
	if len(existing) == 0 {
		if _, statErr := os.Stat(vaultPath); os.IsNotExist(statErr) {
			fmt.Printf("no vault.yml found under %s -- treating every referenced secret as missing\n\n", root)
			vaultPath = ""
		}
	}

	d := scan.DiffAgainst(refs, existing)

	if vaultPath != "" {
		fmt.Printf("vault file: %s\n\n", relOrAbs(root, vaultPath))
	}

	if len(d.Missing) == 0 {
		fmt.Println("MISSING: none -- every referenced vault_ var is defined.")
	} else {
		fmt.Printf("MISSING (%d)\n", len(d.Missing))
		for _, r := range d.Missing {
			printRef(r, "  ")
		}
	}

	if len(d.Unreferenced) > 0 {
		fmt.Printf("\nDEFINED BUT UNREFERENCED (%d) -- nothing under this environment points at these; confirm before removing, something outside this scan (e.g. another environment sharing the value) may still need them.\n", len(d.Unreferenced))
		for _, name := range d.Unreferenced {
			fmt.Printf("  %s\n", name)
		}
	}

	if *verbose && len(d.Present) > 0 {
		fmt.Printf("\nPRESENT (%d)\n", len(d.Present))
		for _, r := range d.Present {
			fmt.Printf("  %s\n", r.Name)
		}
	}

	if len(d.Missing) > 0 {
		return 1
	}
	return 0
}

func printRef(r scan.Reference, indent string) {
	fmt.Printf("%s%s\n", indent, r.Name)
	fmt.Printf("%s  referenced in: %s\n", indent, joinFiles(r.Files))
	if spec, ok := catalog.Lookup(r.Name); ok {
		fmt.Printf("%s  %s\n", indent, spec.Description)
		fmt.Printf("%s  consumed by: %s\n", indent, spec.ConsumedBy)
		fmt.Printf("%s  %s\n", indent, strategyLine(spec))
		if spec.SharedAcrossEnvs {
			fmt.Printf("%s  note: shared across environments -- reuse the same value in testzone and dangerzone\n", indent)
		}
		if spec.Critical {
			fmt.Printf("%s  note: losing this value is unrecoverable -- also keep a copy outside the vault\n", indent)
		}
	} else {
		fmt.Printf("%s  no catalog entry -- paste a value manually\n", indent)
	}
	fmt.Println()
}

func strategyLine(spec catalog.SecretSpec) string {
	switch spec.Strategy {
	case catalog.StrategyRandomBase64:
		n := spec.RandomBytes
		if n == 0 {
			n = 32
		}
		return fmt.Sprintf("generate: random, %d bytes base64-encoded", n)
	case catalog.StrategySha512Crypt:
		return "generate: SHA512-CRYPT hash (openssl passwd -6)"
	case catalog.StrategyExternalCmd:
		line := "generate: external command -- " + spec.ExternalCmdHint
		if spec.DependsOn != "" {
			line += fmt.Sprintf(" (needs %s's plaintext value)", spec.DependsOn)
		}
		return line
	case catalog.StrategyImportOnly:
		return "import only: " + spec.ImportHint
	default:
		return "generate: unknown strategy"
	}
}

func joinFiles(files []string) string {
	return strings.Join(files, ", ")
}

func relOrAbs(base, target string) string {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return target
	}
	return rel
}

func runSet(args []string) int {
	fs := flag.NewFlagSet("set", flag.ExitOnError)
	envDir := fs.String("i", "", "path to an inventory environment (required)")
	dryRun := fs.Bool("dry-run", false, "print the merged vault.yml instead of writing it")
	fs.Parse(args)

	items := fs.Args()
	if *envDir == "" || len(items) == 0 {
		fmt.Fprintln(os.Stderr, "set: -i <inventory-env-dir> and at least one KEY=VALUE or KEY are required")
		return 2
	}
	root, err := filepath.Abs(*envDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "set:", err)
		return 1
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		fmt.Fprintf(os.Stderr, "set: %s is not a directory\n", root)
		return 1
	}

	vaultPath, err := resolveVaultPath(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "set:", err)
		return 1
	}

	v, err := vaultfile.Load(vaultPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "set:", err)
		return 1
	}

	var reveals []secretops.Reveal
	for _, item := range items {
		if key, value, ok := strings.Cut(item, "="); ok {
			if !scan.IsVaultVar(key) {
				fmt.Fprintf(os.Stderr, "set: warning: %q doesn't start with vault_ -- setting it anyway\n", key)
			}
			v.Set(key, value)
			continue
		}

		key := item
		if !scan.IsVaultVar(key) {
			fmt.Fprintf(os.Stderr, "set: %q is neither KEY=VALUE nor a vault_ name to generate\n", key)
			return 2
		}
		spec, ok := catalog.Lookup(key)
		if !ok {
			fmt.Fprintf(os.Stderr, "set: no catalog entry for %s -- provide it explicitly: %s=<value>\n", key, key)
			return 2
		}
		val, rv, err := secretops.Generate(spec, v)
		if err != nil {
			fmt.Fprintln(os.Stderr, "set:", err)
			return 1
		}
		v.Set(key, val)
		if rv != nil {
			reveals = append(reveals, *rv)
		}
	}

	if len(reveals) > 0 {
		fmt.Println("=== SAVE THESE NOW -- shown once, not stored anywhere ===")
		for _, r := range reveals {
			fmt.Printf("  %s: %s\n", r.Label, r.Value)
		}
		fmt.Println()
	}

	if !v.Dirty() {
		fmt.Println("no changes -- every given key already has the given value")
		return 0
	}

	if len(v.Added()) > 0 {
		fmt.Printf("would add:    %s\n", strings.Join(v.Added(), ", "))
	}
	if len(v.Updated()) > 0 {
		fmt.Printf("would update: %s (overwriting an existing value)\n", strings.Join(v.Updated(), ", "))
	}

	rendered, err := v.Render()
	if err != nil {
		fmt.Fprintln(os.Stderr, "set:", err)
		return 1
	}

	if *dryRun {
		fmt.Println("\n--- dry run: rendered vault.yml (not written) ---")
		fmt.Print(rendered)
		return 0
	}

	if v.Existed {
		fmt.Printf("\nbacking up existing file to %s.bak before writing\n", vaultPath)
	}
	if err := v.Commit(); err != nil {
		fmt.Fprintln(os.Stderr, "set:", err)
		return 1
	}
	fmt.Printf("wrote %s\n", vaultPath)
	if v.Existed && !v.WasEncrypted {
		fmt.Println("note: this file was plaintext, not ansible-vault encrypted -- encrypt it before it holds anything real:")
		fmt.Printf("  ansible-vault encrypt %s\n", vaultPath)
	}
	return 0
}
