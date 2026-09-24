// Package tui implements the installer's guided quickstart/update
// wizard: pick an environment, decrypt its vault.yml (if any), walk
// through every vault_* secret it references but hasn't defined yet
// (generate, paste, or skip each one), review, and commit -- the same
// scan/vaultfile/secretops foundation the `scan`/`set` CLI commands
// use, just wrapped in a guided flow instead of flags and arguments.
//
// "Quickstart" and "update" aren't two different code paths: they're
// the same walk over the same gap list (internal/scan.DiffAgainst),
// framed differently depending on whether vault.yml existed yet at all.
package tui

import (
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"homebase/installer/internal/catalog"
	"homebase/installer/internal/scan"
	"homebase/installer/internal/secretops"
	"homebase/installer/internal/svccatalog"
	"homebase/installer/internal/vaultfile"
)

type screen int

const (
	scrModeSelect screen = iota
	scrEnvInput
	scrScanning
	scrDecrypting
	scrSummary
	scrSecretMenu
	scrSecretPaste
	scrSecretGenerating
	scrSecretReveal
	scrReview
	scrCommitting
	scrDone
	scrFatal
	scrServicePicker
	scrServiceReview
)

type actionKind int

const (
	actGenerate actionKind = iota
	actPaste
	actSkip
)

type menuOption struct {
	kind  actionKind
	label string
}

// Model is the wizard's whole state. One struct, one Update -- simplest
// thing that works for a linear wizard with a handful of branches, no
// need for bubbles' full list/table components here.
type Model struct {
	screen screen
	width  int
	height int

	quitting bool

	// screen: mode select (skipped entirely when presetDir is valid --
	// passing -i already says which mode is meant)
	modeCursor int // 0 = fill secrets for an existing env, 1 = start a new one

	// screen: env input
	envInput  textinput.Model
	envErr    error
	presetDir string // from -i; skips straight past scrEnvInput if valid

	// resolved once the environment is confirmed
	root      string
	vaultPath string

	spin spinner.Model

	// scan + vault state
	refs       []scan.Reference
	vault      *vaultfile.Vault
	loadPlan   *vaultfile.LoadPlan
	quickstart bool // vault.yml didn't exist at all when we loaded it

	missing []scan.Reference
	idx     int // index into missing for the secret currently being handled

	menuCursor  int
	menuOptions []menuOption

	pasteInput textinput.Model
	pasteMask  bool

	pendingReveal *secretops.Reveal // set while scrSecretReveal is showing it
	reveals       []secretops.Reveal
	skipped       []string

	cancelled bool

	commitPlan *vaultfile.CommitPlan

	// screen: service picker / review (new-environment mode)
	services    []svccatalog.Service
	svcLoadErr  error
	svcCursor   int
	svcSelected map[string]bool // by Service.Name

	err   error // inline, recoverable (shown on the current screen, doesn't change screen)
	fatal error // unrecoverable -- switches to scrFatal
}

// New builds the wizard's initial model. presetDir pre-fills (and, if
// it's already a valid directory, skips past) the environment picker --
// e.g. from `installer wizard -i <dir>`.
func New(presetDir string) Model {
	ti := textinput.New()
	ti.Placeholder = "../ansible-playbooks/inventory/testzone"
	ti.Prompt = "Environment directory: "
	ti.CharLimit = 4096
	ti.Focus()
	if presetDir != "" {
		ti.SetValue(presetDir)
		ti.CursorEnd()
	}

	pi := textinput.New()
	pi.Prompt = "Value: "
	pi.CharLimit = 0
	pi.EchoMode = textinput.EchoPassword
	pi.EchoCharacter = '•'

	sp := spinner.New(spinner.WithSpinner(spinner.Dot))

	// -i already answers "which mode" (fill secrets for a specific,
	// named environment), so skip the mode-select fork entirely in that
	// case -- same fast path as before the picker existed.
	startScreen := scrModeSelect
	if presetDir != "" {
		startScreen = scrEnvInput
	}

	return Model{
		screen:     startScreen,
		envInput:   ti,
		presetDir:  presetDir,
		pasteInput: pi,
		pasteMask:  true,
		spin:       sp,
	}
}

func (m Model) Init() tea.Cmd {
	if m.presetDir != "" && isDir(m.presetDir) {
		root, err := absPath(m.presetDir)
		if err != nil {
			return nil
		}
		return tea.Batch(startScan(root), spinnerTick(m))
	}
	if m.screen == scrEnvInput {
		return textinput.Blink
	}
	return nil
}

// currentService looks up services[svcCursor], if any.
func (m Model) currentService() (svccatalog.Service, bool) {
	if m.svcCursor < 0 || m.svcCursor >= len(m.services) {
		return svccatalog.Service{}, false
	}
	return m.services[m.svcCursor], true
}

func (m Model) selectedServiceCount() int {
	n := 0
	for _, sel := range m.svcSelected {
		if sel {
			n++
		}
	}
	return n
}

func (m Model) selectedServices() []svccatalog.Service {
	var out []svccatalog.Service
	for _, s := range m.services {
		if m.svcSelected[s.Name] {
			out = append(out, s)
		}
	}
	return out
}

// withServicesLoaded loads the service catalog and pre-selects every
// Core-flagged entry, ready for scrServicePicker. Pure/fast (an
// embed.FS read + YAML parse of a handful of files), so it runs
// directly in an Update handler rather than as a tea.Cmd.
func (m Model) withServicesLoaded() Model {
	services, err := svccatalog.Load()
	m.services = services
	m.svcLoadErr = err
	m.svcSelected = map[string]bool{}
	for _, s := range services {
		if s.Core {
			m.svcSelected[s.Name] = true
		}
	}
	m.svcCursor = 0
	return m
}

// currentSpec looks up the catalog entry for missing[idx], if any.
func (m Model) currentRef() (scan.Reference, bool) {
	if m.idx < 0 || m.idx >= len(m.missing) {
		return scan.Reference{}, false
	}
	return m.missing[m.idx], true
}

func (m Model) currentSpec() (catalog.SecretSpec, bool) {
	ref, ok := m.currentRef()
	if !ok {
		return catalog.SecretSpec{}, false
	}
	return catalog.Lookup(ref.Name)
}

func diffMissing(refs []scan.Reference, v *vaultfile.Vault) []scan.Reference {
	var out []scan.Reference
	for _, r := range refs {
		if !v.Has(r.Name) {
			out = append(out, r)
		}
	}
	return out
}

func (m Model) isSpinning() bool {
	switch m.screen {
	case scrScanning, scrDecrypting, scrSecretGenerating, scrCommitting:
		return true
	default:
		return false
	}
}

func buildMenu(spec catalog.SecretSpec, haveCatalogEntry bool) []menuOption {
	var opts []menuOption
	if haveCatalogEntry && secretops.Generatable(spec) {
		opts = append(opts, menuOption{actGenerate, "Generate"})
	}
	opts = append(opts, menuOption{actPaste, "Paste a value"})
	opts = append(opts, menuOption{actSkip, "Skip for now"})
	return opts
}
