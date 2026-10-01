package tui

import (
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
)

func BitriseTheme() huh.Theme {
	return huh.ThemeFunc(func(isDark bool) *huh.Styles {
		var (
			bitrisePurple30  = lipgloss.Color("#5c2a7e")
			bitrisePurple60  = lipgloss.Color("#ae63de")
			bitriseGreen60   = lipgloss.Color("#4eb76c")
			bitriseRed60     = lipgloss.Color("#f7596c")
			bitriseNeutral70 = lipgloss.Color("#afa4b4")
			bitriseNeutral40 = lipgloss.Color("#6b6071")
			bitriseWhite     = lipgloss.Color("#ffffff")
		)

		s := huh.ThemeBase(isDark)

		highlight := lipgloss.NewStyle().Background(bitrisePurple30).Foreground(bitriseWhite)

		s.Focused.Title = s.Focused.Title.Foreground(bitrisePurple60).Bold(true)
		s.Focused.NoteTitle = s.Focused.NoteTitle.Foreground(bitrisePurple60).Bold(true)
		s.Focused.Description = s.Focused.Description.Foreground(bitriseNeutral70)
		s.Focused.ErrorIndicator = lipgloss.NewStyle().Foreground(bitriseRed60).SetString(" ✗")
		s.Focused.ErrorMessage = s.Focused.ErrorMessage.Foreground(bitriseRed60)

		s.Focused.SelectSelector = lipgloss.NewStyle().Foreground(bitrisePurple60).SetString("❯ ")
		s.Focused.MultiSelectSelector = lipgloss.NewStyle().Foreground(bitrisePurple60).SetString("❯ ")
		s.Focused.SelectedOption = highlight
		s.Focused.SelectedPrefix = lipgloss.NewStyle().Foreground(bitriseGreen60).SetString("[x] ")
		s.Focused.UnselectedPrefix = lipgloss.NewStyle().Foreground(bitriseNeutral70).SetString("[ ] ")
		s.Focused.FocusedButton = highlight.Bold(true).Padding(0, 2)
		s.Focused.BlurredButton = lipgloss.NewStyle().Foreground(bitriseNeutral70).Background(bitriseNeutral40).Padding(0, 2)
		s.Focused.Next = s.Focused.FocusedButton

		s.Focused.TextInput.Prompt = s.Focused.TextInput.Prompt.Foreground(bitrisePurple60)
		s.Focused.TextInput.Cursor = s.Focused.TextInput.Cursor.Foreground(bitriseGreen60)
		s.Focused.TextInput.Placeholder = s.Focused.TextInput.Placeholder.Foreground(bitriseNeutral40)

		s.Blurred = s.Focused
		s.Blurred.Title = s.Blurred.Title.Foreground(bitriseNeutral70)
		s.Blurred.Description = s.Blurred.Description.Foreground(bitriseNeutral40)
		s.Blurred.SelectSelector = lipgloss.NewStyle().SetString("  ")
		s.Blurred.MultiSelectSelector = lipgloss.NewStyle().SetString("  ")
		s.Blurred.SelectedPrefix = lipgloss.NewStyle().Foreground(bitriseNeutral40).SetString("[x] ")
		s.Blurred.UnselectedPrefix = lipgloss.NewStyle().Foreground(bitriseNeutral40).SetString("[ ] ")
		s.Blurred.SelectedOption = lipgloss.NewStyle().Foreground(bitriseNeutral70)
		s.Blurred.Next = s.Focused.BlurredButton

		s.Group.Title = s.Focused.Title
		s.Group.Description = s.Focused.Description
		s.Help.ShortKey = s.Help.ShortKey.Foreground(bitrisePurple60)
		s.Help.ShortDesc = s.Help.ShortDesc.Foreground(bitriseNeutral70)
		s.Help.FullKey = s.Help.FullKey.Foreground(bitrisePurple60)
		s.Help.FullDesc = s.Help.FullDesc.Foreground(bitriseNeutral70)

		return s
	})
}
