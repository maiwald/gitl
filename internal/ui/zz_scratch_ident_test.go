package ui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestScratchRenderIdentity(t *testing.T) {
	cases := []string{
		"",
		"plain text",
		" +context line with  spaces ",
		diffAddStyle.Render("+added") + diffContextStyle.Render(" tail"),
		searchMatchStyle.Render("hit") + "rest" + shaStyle.Render("abc"),
		"unicode ünïcödé ✓ 日本語",
		selectedRowStyle.Render("sel"),
	}
	for _, s := range cases {
		for _, w := range []int{0, 5, 40, 200} {
			if w < lipgloss.Width(s) {
				continue
			}
			if got := lipgloss.NewStyle().MaxWidth(w).Render(s); got != s {
				t.Errorf("MaxWidth(%d).Render(%q) = %q, not identity", w, s, got)
			}
		}
	}
	for _, n := range []int{1, 3, 17} {
		s := ""
		for i := 0; i < n; i++ {
			s += " "
		}
		if got := lipgloss.NewStyle().Render(s); got != s {
			t.Errorf("NewStyle().Render(%d spaces) = %q, not identity", n, got)
		}
	}
}
