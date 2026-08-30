package ui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/tui-tools/tui-kit/compat"
	"github.com/tui-tools/tui-kit/theme"
)

// CompatBadge renders the backend and its version for the header, with the
// qualifier that says how much we know about that version:
//
//	ufw 0.36.2                       tested, nothing to say
//	ufw 0.37.0 (untested)            warning colour
//	ufw 0.35 (below minimum 0.36)    error colour
//	ufw (version unknown)            muted, the probe found nothing
//
// The name and the version are rendered in the base style and only the
// qualifier is coloured, so a normal machine shows a quiet header and an
// unusual one is visible without reading it.
func CompatBadge(t theme.Theme, r compat.Result) string {
	name := r.Backend
	if name == "" {
		name = "backend"
	}
	if r.Version == "" {
		return t.Base.Render(name) + " " + t.Muted.Render("(version unknown)")
	}

	text := t.Base.Render(name + " " + r.Version)
	if qualifier, style := compatQualifier(t, r); qualifier != "" {
		text += " " + style.Render("("+qualifier+")")
	}
	return text
}

// CompatFact is the same information as one header Fact. A Fact carries a
// single style, so the qualifier's colour is applied to the whole value: it is
// the version that is unusual, not just the word for it.
func CompatFact(t theme.Theme, r compat.Result) Fact {
	if r.Version == "" {
		style := t.Muted
		return Fact{Label: "backend", Value: r.Backend + " (version unknown)",
			Style: &style}
	}

	value := r.Backend + " " + r.Version
	qualifier, style := compatQualifier(t, r)
	if qualifier == "" {
		return Fact{Label: "backend", Value: value}
	}
	return Fact{Label: "backend", Value: value + " (" + qualifier + ")",
		Style: &style}
}

// compatQualifier is the parenthesised half of the badge and the style it is
// drawn in. A tested version has none.
func compatQualifier(t theme.Theme, r compat.Result) (string, lipgloss.Style) {
	switch r.Status {
	case compat.StatusBelowMinimum:
		text := "below minimum"
		if r.Minimum != "" {
			text += " " + r.Minimum
		}
		return text, t.Danger
	case compat.StatusUntested:
		return "untested", t.Warn
	case compat.StatusUnknown:
		return "version unknown", t.Muted
	default:
		return "", t.Base
	}
}

// CompatNotes renders the manifest notes that apply to the running version,
// one line each, for a help screen or a status hint. It returns nil when the
// version has no caveats, so a caller can test the length.
func CompatNotes(t theme.Theme, r compat.Result) []string {
	var lines []string
	for _, note := range r.Notes {
		lines = append(lines, t.Warn.Render(note.Range)+" "+t.Muted.Render(note.Impact))
	}
	return lines
}
