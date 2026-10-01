package tui

import (
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
)

func BitriseTheme() huh.Theme {
	return huh.ThemeFunc(func(isDark bool) *huh.Styles {
		var (
			bitrisePurple30     = lipgloss.Color("#5c2a7e")
			bitrisePurple60     = lipgloss.Color("#ae63de")
			bitrisePurpleAccent = lipgloss.Color("#7b3ba5")
			bitriseGreen60      = lipgloss.Color("#4eb76c")
			bitriseGreenAccent  = lipgloss.Color("#167231")
			bitriseRed60        = lipgloss.Color("#f7596c")
			bitriseRedAccent    = lipgloss.Color("#a91e2e")
			bitriseNeutral90    = lipgloss.Color("#dfdae1")
			bitriseNeutral70    = lipgloss.Color("#afa4b4")
			bitriseNeutral40    = lipgloss.Color("#6b6071")
			bitriseNeutral20    = lipgloss.Color("#362d39")
			bitriseWhite        = lipgloss.Color("#ffffff")
		)

		accent := bitrisePurple60
		muted := bitriseNeutral70
		mutedDim := bitriseNeutral40
		checkFg := bitriseGreen60
		errFg := bitriseRed60
		if !isDark {
			accent = bitrisePurpleAccent
			muted = bitriseNeutral40
			mutedDim = bitriseNeutral40
			checkFg = bitriseGreenAccent
			errFg = bitriseRedAccent
		}

		s := huh.ThemeBase(isDark)

		highlight := lipgloss.NewStyle().Background(bitrisePurple30).Foreground(bitriseWhite)

		buttonBlurBg := bitriseNeutral20
		buttonBlurFg := bitriseNeutral90
		if !isDark {
			buttonBlurBg = bitriseNeutral90
			buttonBlurFg = bitriseNeutral20
		}

		s.Focused.Title = s.Focused.Title.Foreground(accent).Bold(true)
		s.Focused.NoteTitle = s.Focused.NoteTitle.Foreground(accent).Bold(true)
		s.Focused.Description = s.Focused.Description.Foreground(muted)
		s.Focused.ErrorIndicator = lipgloss.NewStyle().Foreground(errFg).SetString(" ✗")
		s.Focused.ErrorMessage = s.Focused.ErrorMessage.Foreground(errFg)

		s.Focused.SelectSelector = lipgloss.NewStyle().Foreground(accent).SetString("❯ ")
		s.Focused.MultiSelectSelector = lipgloss.NewStyle().Foreground(accent).SetString("❯ ")
		s.Focused.SelectedOption = lipgloss.NewStyle().Foreground(accent).Bold(true)
		s.Focused.SelectedPrefix = lipgloss.NewStyle().Foreground(checkFg).SetString("[x] ")
		s.Focused.UnselectedPrefix = lipgloss.NewStyle().Foreground(muted).SetString("[ ] ")
		s.Focused.FocusedButton = highlight.Bold(true).Padding(0, 2)
		s.Focused.BlurredButton = lipgloss.NewStyle().Foreground(buttonBlurFg).Background(buttonBlurBg).Padding(0, 2)
		s.Focused.Next = s.Focused.FocusedButton

		s.Focused.TextInput.Prompt = s.Focused.TextInput.Prompt.Foreground(accent)
		s.Focused.TextInput.Cursor = s.Focused.TextInput.Cursor.Foreground(checkFg)
		s.Focused.TextInput.Placeholder = s.Focused.TextInput.Placeholder.Foreground(mutedDim)

		s.Blurred = s.Focused
		s.Blurred.Title = s.Blurred.Title.Foreground(muted)
		s.Blurred.Description = s.Blurred.Description.Foreground(mutedDim)
		s.Blurred.SelectSelector = lipgloss.NewStyle().SetString("  ")
		s.Blurred.MultiSelectSelector = lipgloss.NewStyle().SetString("  ")
		s.Blurred.SelectedPrefix = lipgloss.NewStyle().Foreground(mutedDim).SetString("[x] ")
		s.Blurred.UnselectedPrefix = lipgloss.NewStyle().Foreground(mutedDim).SetString("[ ] ")
		s.Blurred.SelectedOption = lipgloss.NewStyle().Foreground(muted)
		s.Blurred.Next = s.Focused.BlurredButton

		s.Group.Title = s.Focused.Title
		s.Group.Description = s.Focused.Description
		s.Help.ShortKey = s.Help.ShortKey.Foreground(accent)
		s.Help.ShortDesc = s.Help.ShortDesc.Foreground(muted)
		s.Help.FullKey = s.Help.FullKey.Foreground(accent)
		s.Help.FullDesc = s.Help.FullDesc.Foreground(muted)

		return s
	})
}
