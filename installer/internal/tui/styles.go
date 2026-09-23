package tui

import "github.com/charmbracelet/lipgloss"

// Colors are deliberately restrained: bold/faint/a couple of accent
// colors, nothing that fights a user's own terminal theme.
var (
	styleTitle  = lipgloss.NewStyle().Bold(true)
	styleFaint  = lipgloss.NewStyle().Faint(true)
	styleAccent = lipgloss.NewStyle().Foreground(lipgloss.Color("6")) // cyan
	styleGood   = lipgloss.NewStyle().Foreground(lipgloss.Color("2")) // green
	styleWarn   = lipgloss.NewStyle().Foreground(lipgloss.Color("3")) // yellow
	styleBad    = lipgloss.NewStyle().Foreground(lipgloss.Color("1")) // red

	styleCursor = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)

	styleBanner = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("0")).
			Background(lipgloss.Color("3")).
			Padding(0, 1)

	styleBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("8")).
			Padding(0, 1)
)

func cursorLine(selected bool, label string) string {
	if selected {
		return styleCursor.Render("> ") + label
	}
	return "  " + label
}
