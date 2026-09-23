package tui

import (
	"fmt"
	"sort"
	"strings"

	"homebase/installer/internal/catalog"
)

func (m Model) View() string {
	if m.quitting {
		return ""
	}

	header := styleBanner.Render(" Homebase Secrets Wizard ") + "\n\n"

	var body string
	switch m.screen {
	case scrModeSelect:
		body = m.viewModeSelect()
	case scrServicePicker:
		body = m.viewServicePicker()
	case scrServiceReview:
		body = m.viewServiceReview()
	case scrEnvInput:
		body = m.viewEnvInput()
	case scrScanning:
		body = m.spin.View() + " scanning for vault_ references...\n"
	case scrDecrypting:
		body = m.spin.View() + " decrypting " + relPath(m) + " -- enter the vault password if prompted\n"
	case scrSummary:
		body = m.viewSummary()
	case scrSecretMenu:
		body = m.viewSecretMenu()
	case scrSecretPaste:
		body = m.viewSecretPaste()
	case scrSecretGenerating:
		body = m.viewSecretGenerating()
	case scrSecretReveal:
		body = m.viewSecretReveal()
	case scrReview:
		body = m.viewReview()
	case scrCommitting:
		body = m.spin.View() + " writing " + relPath(m) + " -- you may be prompted for the vault password\n"
	case scrDone:
		body = m.viewDone()
	case scrFatal:
		body = styleBad.Render("Error: "+m.fatal.Error()) + "\n\n" + styleFaint.Render("press any key to exit")
	}

	return header + body
}

func relPath(m Model) string {
	if m.vaultPath == "" {
		return "vault.yml"
	}
	return m.vaultPath
}

func (m Model) viewEnvInput() string {
	var b strings.Builder
	b.WriteString(styleFaint.Render("Point this at an ansible-playbooks inventory environment -- e.g. ansible-playbooks/inventory/testzone.") + "\n\n")
	b.WriteString(m.envInput.View() + "\n")
	if m.envErr != nil {
		b.WriteString("\n" + styleBad.Render(m.envErr.Error()) + "\n")
	}
	b.WriteString("\n" + styleFaint.Render("[enter] continue   [esc] quit"))
	return b.String()
}

func (m Model) viewSummary() string {
	var b strings.Builder
	if m.quickstart {
		b.WriteString(styleTitle.Render("Quickstart") + "\n")
		b.WriteString(m.vaultPath + " doesn't exist yet.\n\n")
	} else {
		b.WriteString(styleTitle.Render("Update") + "\n")
		b.WriteString(m.vaultPath + " already exists.\n\n")
	}

	if len(m.missing) == 0 {
		b.WriteString(styleGood.Render("Every vault_ variable this environment references is already defined.") + "\n")
		b.WriteString("\n" + styleFaint.Render("[enter] exit"))
		return b.String()
	}

	word := "secret"
	if len(m.missing) != 1 {
		word = "secrets"
	}
	b.WriteString(fmt.Sprintf("This environment references %d %s it hasn't defined yet:\n\n", len(m.missing), word))
	for _, r := range m.missing {
		b.WriteString("  - " + r.Name + "\n")
	}
	b.WriteString("\n" + styleFaint.Render("We'll go through them one at a time -- generate, paste, or skip each.") + "\n")
	b.WriteString("\n" + styleFaint.Render("[enter] begin   [q] quit"))
	return b.String()
}

func (m Model) secretHeader() string {
	ref, ok := m.currentRef()
	if !ok {
		return ""
	}
	var b strings.Builder
	b.WriteString(styleFaint.Render(fmt.Sprintf("(%d/%d) ", m.idx+1, len(m.missing))))
	b.WriteString(styleTitle.Render(ref.Name) + "\n")

	spec, found := catalog.Lookup(ref.Name)
	if found {
		b.WriteString(spec.Description + "\n")
		b.WriteString(styleFaint.Render("consumed by: "+spec.ConsumedBy) + "\n")
		if spec.SharedAcrossEnvs {
			b.WriteString(styleWarn.Render("shared across environments -- use the same value in testzone and dangerzone") + "\n")
		}
		if spec.Critical {
			b.WriteString(styleWarn.Render("losing this value is unrecoverable -- keep a copy outside the vault too") + "\n")
		}
	} else {
		b.WriteString(styleFaint.Render("no catalog entry -- paste a value manually") + "\n")
	}
	b.WriteString(styleFaint.Render("referenced in: "+strings.Join(ref.Files, ", ")) + "\n")
	return b.String()
}

func (m Model) viewSecretMenu() string {
	var b strings.Builder
	b.WriteString(m.secretHeader())
	b.WriteString("\n")
	for i, opt := range m.menuOptions {
		b.WriteString(cursorLine(i == m.menuCursor, opt.label) + "\n")
	}
	if m.err != nil {
		b.WriteString("\n" + styleBad.Render(m.err.Error()) + "\n")
	}
	b.WriteString("\n" + styleFaint.Render("[up/down] choose   [enter] select   [q] quit"))
	return b.String()
}

func (m Model) viewSecretPaste() string {
	var b strings.Builder
	b.WriteString(m.secretHeader())
	b.WriteString("\n" + m.pasteInput.View() + "\n")
	if m.err != nil {
		b.WriteString("\n" + styleBad.Render(m.err.Error()) + "\n")
	}
	maskHint := "show"
	if !m.pasteMask {
		maskHint = "hide"
	}
	b.WriteString("\n" + styleFaint.Render(fmt.Sprintf("[enter] save   [esc] back   [ctrl+r] %s value", maskHint)))
	return b.String()
}

func (m Model) viewSecretGenerating() string {
	var b strings.Builder
	b.WriteString(m.secretHeader())
	b.WriteString("\n" + m.spin.View() + " generating...\n")
	return b.String()
}

func (m Model) viewSecretReveal() string {
	if m.pendingReveal == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(styleBanner.Render(" SAVE THIS NOW -- shown once, not stored anywhere ") + "\n\n")
	b.WriteString(m.pendingReveal.Label + ":\n")
	b.WriteString(styleBox.Render(m.pendingReveal.Value) + "\n")
	b.WriteString("\n" + styleFaint.Render("[enter] I've saved it, continue"))
	return b.String()
}

func (m Model) viewReview() string {
	var b strings.Builder
	b.WriteString(styleTitle.Render("Review") + "\n\n")

	if added := m.vault.Added(); len(added) > 0 {
		b.WriteString(styleGood.Render(fmt.Sprintf("Add (%d):", len(added))) + "\n")
		for _, k := range added {
			b.WriteString("  + " + k + " " + maskedPreview(m, k) + "\n")
		}
	}
	if updated := m.vault.Updated(); len(updated) > 0 {
		b.WriteString(styleWarn.Render(fmt.Sprintf("Update (%d):", len(updated))) + "\n")
		for _, k := range updated {
			b.WriteString("  ~ " + k + " " + maskedPreview(m, k) + "\n")
		}
	}
	if len(m.skipped) > 0 {
		b.WriteString(styleFaint.Render(fmt.Sprintf("Skipped (%d): %s", len(m.skipped), strings.Join(m.skipped, ", "))) + "\n")
	}
	if len(m.reveals) > 0 {
		b.WriteString("\n" + styleWarn.Render("Reminder -- make sure you saved these already:") + "\n")
		for _, r := range m.reveals {
			b.WriteString("  - " + r.Label + "\n")
		}
	}

	b.WriteString("\n" + styleFaint.Render("[enter] write to vault.yml   [c] cancel without saving   [q] quit"))
	return b.String()
}

func maskedPreview(m Model, key string) string {
	val, ok := m.vault.Get(key)
	if !ok || val == "" {
		return ""
	}
	if len(val) <= 4 {
		return styleFaint.Render("(****)")
	}
	return styleFaint.Render("(****" + val[len(val)-4:] + ")")
}

func (m Model) viewDone() string {
	var b strings.Builder
	if m.cancelled {
		b.WriteString(styleWarn.Render("Cancelled -- nothing was written.") + "\n")
		b.WriteString("\n" + styleFaint.Render("[any key] exit"))
		return b.String()
	}

	b.WriteString(styleGood.Render("Done.") + "\n\n")
	b.WriteString("Wrote " + m.vaultPath + "\n")
	if m.vault.Existed {
		b.WriteString(styleFaint.Render("(backed up the previous version to "+m.vaultPath+".bak)") + "\n")
	}
	if len(m.reveals) > 0 {
		b.WriteString("\n" + styleWarn.Render("Reminder -- make sure you saved these:") + "\n")
		for _, r := range m.reveals {
			b.WriteString("  - " + r.Label + "\n")
		}
	}
	b.WriteString("\n" + styleFaint.Render("[any key] exit"))
	return b.String()
}

func (m Model) viewModeSelect() string {
	var b strings.Builder
	b.WriteString(styleFaint.Render("What would you like to do?") + "\n\n")
	b.WriteString(cursorLine(m.modeCursor == 0, "Fill in secrets for an existing environment") + "\n")
	b.WriteString(cursorLine(m.modeCursor == 1, "Start a new environment (pick services)") + "\n")
	b.WriteString("\n" + styleFaint.Render("[up/down] choose   [enter] select   [q] quit"))
	return b.String()
}

func (m Model) viewServicePicker() string {
	if m.svcLoadErr != nil {
		return styleBad.Render("Error loading service catalog: "+m.svcLoadErr.Error()) + "\n\n" + styleFaint.Render("[q] quit")
	}

	var b strings.Builder
	b.WriteString(styleTitle.Render("Pick services") + "\n")
	b.WriteString(styleFaint.Render(fmt.Sprintf("%d/%d selected", m.selectedServiceCount(), len(m.services))) + "\n\n")

	for i, s := range m.services {
		box := "[ ]"
		if m.svcSelected[s.Name] {
			box = "[x]"
		}
		line := box + " " + s.Name
		if s.DedicatedHost {
			line += styleFaint.Render(" (wants its own host)")
		}
		b.WriteString(cursorLine(i == m.svcCursor, line) + "\n")
	}

	b.WriteString("\n")
	if svc, ok := m.currentService(); ok {
		b.WriteString(styleFaint.Render(svc.Description) + "\n")
	}

	b.WriteString("\n" + styleFaint.Render("[up/down] move   [space] toggle   [a] all   [n] none   [enter] continue   [esc] back   [q] quit"))
	return b.String()
}

func (m Model) viewServiceReview() string {
	var b strings.Builder
	b.WriteString(styleTitle.Render("Selection preview") + "\n")
	b.WriteString(styleWarn.Render("Generating hosts.ini/host_vars from this isn't built yet -- this just previews what the selection implies.") + "\n\n")

	chosen := m.selectedServices()
	if len(chosen) == 0 {
		b.WriteString(styleFaint.Render("Nothing selected.") + "\n")
	}

	secretSet := map[string]bool{}
	for _, s := range chosen {
		line := "- " + s.Name
		if s.DedicatedHost {
			line += styleFaint.Render(" (wants its own host)")
		}
		b.WriteString(line + "\n")
		for _, ref := range s.SecretRefs() {
			secretSet[ref] = true
		}
	}

	if len(secretSet) > 0 {
		names := make([]string, 0, len(secretSet))
		for k := range secretSet {
			names = append(names, k)
		}
		sort.Strings(names)
		b.WriteString("\n" + styleFaint.Render(fmt.Sprintf("This selection will need %d secret(s) once generation lands:", len(names))) + "\n")
		for _, n := range names {
			b.WriteString("  - " + n + "\n")
		}
	}

	b.WriteString("\n" + styleFaint.Render("[esc] back to picker   [enter]/[q] exit"))
	return b.String()
}
