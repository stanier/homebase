package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			m.quitting = true
			return m, tea.Quit
		}

	case spinner.TickMsg:
		if !m.isSpinning() {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case scanResultMsg:
		return m.onScanResult(msg)

	case vaultLoadedMsg:
		return m.onVaultLoaded(msg)

	case generateResultMsg:
		return m.onGenerateResult(msg)

	case commitPlanMsg:
		return m.onCommitPlan(msg)

	case commitDoneMsg:
		if msg.err != nil {
			m.fatal = msg.err
			m.screen = scrFatal
			return m, nil
		}
		m.screen = scrDone
		return m, nil
	}

	switch m.screen {
	case scrModeSelect:
		return m.updateModeSelect(msg)
	case scrServicePicker:
		return m.updateServicePicker(msg)
	case scrServiceReview:
		return m.updateServiceReview(msg)
	case scrEnvInput:
		return m.updateEnvInput(msg)
	case scrSummary:
		return m.updateSummary(msg)
	case scrSecretMenu:
		return m.updateSecretMenu(msg)
	case scrSecretPaste:
		return m.updateSecretPaste(msg)
	case scrSecretReveal:
		return m.updateSecretReveal(msg)
	case scrReview:
		return m.updateReview(msg)
	case scrDone, scrFatal:
		return m.updateExit(msg)
	}
	return m, nil
}

func (m Model) onScanResult(msg scanResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.fatal = msg.err
		m.screen = scrFatal
		return m, nil
	}
	m.root = msg.root
	m.vaultPath = msg.vaultPath
	m.refs = msg.refs
	m.loadPlan = msg.loadPlan

	if cmd := msg.loadPlan.Cmd(); cmd != nil {
		m.screen = scrDecrypting
		plan := msg.loadPlan
		path := msg.vaultPath
		return m, tea.Batch(m.spin.Tick, tea.ExecProcess(cmd, func(err error) tea.Msg {
			if err != nil {
				return vaultLoadedMsg{err: fmt.Errorf("ansible-vault view %s: %w", path, err)}
			}
			return finishLoad(plan)
		}))
	}

	plan := msg.loadPlan
	return m, func() tea.Msg { return finishLoad(plan) }
}

func (m Model) onVaultLoaded(msg vaultLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.fatal = msg.err
		m.screen = scrFatal
		return m, nil
	}
	m.vault = msg.vault
	m.quickstart = !msg.vault.Existed
	m.missing = orderForDependencies(diffMissing(m.refs, msg.vault))
	m.idx = -1 // advance() increments to 0 the first time it's called
	m.screen = scrSummary
	return m, nil
}

func (m Model) onGenerateResult(msg generateResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.err = msg.err
		m.screen = scrSecretMenu
		return m, nil
	}
	m.vault.Set(msg.key, msg.value)
	if msg.reveal != nil {
		m.pendingReveal = msg.reveal
		m.reveals = append(m.reveals, *msg.reveal)
		m.screen = scrSecretReveal
		return m, nil
	}
	return m.advance()
}

func (m Model) onCommitPlan(msg commitPlanMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.fatal = msg.err
		m.screen = scrFatal
		return m, nil
	}
	m.commitPlan = msg.plan
	if msg.plan.Cmd == nil {
		msg.plan.Done()
		m.screen = scrDone
		return m, nil
	}
	plan := msg.plan
	return m, tea.ExecProcess(plan.Cmd, func(err error) tea.Msg {
		plan.Done()
		return commitDoneMsg{err: err}
	})
}

// advance moves to the next missing secret, or to the review screen once
// they're all handled.
func (m Model) advance() (tea.Model, tea.Cmd) {
	m.idx++
	m.err = nil
	if m.idx >= len(m.missing) {
		m.screen = scrReview
		return m, nil
	}
	spec, ok := m.currentSpec()
	m.menuOptions = buildMenu(spec, ok)
	m.menuCursor = 0
	m.screen = scrSecretMenu
	return m, nil
}

func (m Model) updateEnvInput(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.Type {
		case tea.KeyEnter:
			path := strings.TrimSpace(m.envInput.Value())
			if path == "" {
				m.envErr = fmt.Errorf("enter a path")
				return m, nil
			}
			if !isDir(path) {
				m.envErr = fmt.Errorf("%s is not a directory", path)
				return m, nil
			}
			root, err := absPath(path)
			if err != nil {
				m.envErr = err
				return m, nil
			}
			m.envErr = nil
			m.screen = scrScanning
			return m, tea.Batch(startScan(root), m.spin.Tick)
		case tea.KeyEsc:
			m.quitting = true
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.envInput, cmd = m.envInput.Update(msg)
	return m, cmd
}

func (m Model) updateSummary(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "enter":
		if len(m.missing) == 0 {
			m.quitting = true
			return m, tea.Quit
		}
		return m.advance()
	case "q", "esc":
		m.quitting = true
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) updateSecretMenu(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "up", "k":
		if m.menuCursor > 0 {
			m.menuCursor--
		}
	case "down", "j":
		if m.menuCursor < len(m.menuOptions)-1 {
			m.menuCursor++
		}
	case "enter":
		ref, ok := m.currentRef()
		if !ok || len(m.menuOptions) == 0 {
			return m, nil
		}
		switch m.menuOptions[m.menuCursor].kind {
		case actGenerate:
			spec, _ := m.currentSpec()
			m.err = nil
			m.screen = scrSecretGenerating
			return m, tea.Batch(m.spin.Tick, runGenerate(ref.Name, spec, m.vault))
		case actPaste:
			m.pasteInput.SetValue("")
			m.pasteInput.EchoMode = textinput.EchoPassword
			m.pasteMask = true
			m.pasteInput.Focus()
			m.err = nil
			m.screen = scrSecretPaste
			return m, textinput.Blink
		case actSkip:
			m.skipped = append(m.skipped, ref.Name)
			return m.advance()
		}
	case "q":
		m.quitting = true
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) updateSecretPaste(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.Type {
		case tea.KeyEsc:
			m.err = nil
			m.screen = scrSecretMenu
			return m, nil
		case tea.KeyEnter:
			val := m.pasteInput.Value()
			if val == "" {
				m.err = fmt.Errorf("value can't be empty -- Esc to go back and skip instead")
				return m, nil
			}
			ref, _ := m.currentRef()
			m.vault.Set(ref.Name, val)
			m.pasteInput.Blur()
			return m.advance()
		case tea.KeyCtrlR:
			m.pasteMask = !m.pasteMask
			if m.pasteMask {
				m.pasteInput.EchoMode = textinput.EchoPassword
			} else {
				m.pasteInput.EchoMode = textinput.EchoNormal
			}
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.pasteInput, cmd = m.pasteInput.Update(msg)
	return m, cmd
}

func (m Model) updateSecretReveal(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	if key.Type == tea.KeyEnter || key.String() == "q" {
		m.pendingReveal = nil
		return m.advance()
	}
	return m, nil
}

func (m Model) updateReview(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "enter":
		m.screen = scrCommitting
		return m, tea.Batch(m.spin.Tick, prepareCommit(m.vault))
	case "c":
		m.cancelled = true
		m.screen = scrDone
		return m, nil
	case "q", "esc":
		m.quitting = true
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) updateExit(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(tea.KeyMsg); ok {
		m.quitting = true
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) updateModeSelect(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "up", "k":
		if m.modeCursor > 0 {
			m.modeCursor--
		}
	case "down", "j":
		if m.modeCursor < 1 {
			m.modeCursor++
		}
	case "enter":
		if m.modeCursor == 0 {
			m.screen = scrEnvInput
			return m, textinput.Blink
		}
		m = m.withServicesLoaded()
		m.screen = scrServicePicker
		return m, nil
	case "q", "esc", "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) updateServicePicker(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "up", "k":
		if m.svcCursor > 0 {
			m.svcCursor--
		}
	case "down", "j":
		if m.svcCursor < len(m.services)-1 {
			m.svcCursor++
		}
	case " ":
		if svc, ok := m.currentService(); ok {
			m.svcSelected[svc.Name] = !m.svcSelected[svc.Name]
		}
	case "a":
		for _, s := range m.services {
			m.svcSelected[s.Name] = true
		}
	case "n":
		for _, s := range m.services {
			m.svcSelected[s.Name] = false
		}
	case "enter":
		m.screen = scrServiceReview
		return m, nil
	case "esc":
		m.screen = scrModeSelect
		return m, nil
	case "q", "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) updateServiceReview(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "esc":
		m.screen = scrServicePicker
		return m, nil
	case "q", "enter", "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	}
	return m, nil
}
