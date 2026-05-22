// Package tui is the Bubble Tea TUI for sshiitake. It subscribes to a
// manager.Manager event stream and renders an interactive list/detail
// view of tunnels with status, metrics, and a help overlay.
package tui

import "github.com/charmbracelet/lipgloss"

// Theme is the bundle of lipgloss styles used by the TUI.
type Theme struct {
	StatusUp         lipgloss.Style
	StatusDown       lipgloss.Style
	StatusConnecting lipgloss.Style
	GroupHeader      lipgloss.Style
	TunnelName       lipgloss.Style
	LatencyGood      lipgloss.Style
	LatencyWarn      lipgloss.Style
	LatencyBad       lipgloss.Style
	HelpText         lipgloss.Style
	ErrorText        lipgloss.Style
	Accent           lipgloss.Style
	Border           lipgloss.Style
}

// DefaultThemeName is the theme used when none is specified.
const DefaultThemeName = "dark"

// ThemeByName returns the named theme. Supported: "dark", "light",
// "high-contrast".
func ThemeByName(name string) (Theme, bool) {
	switch name {
	case "dark":
		return darkTheme(), true
	case "light":
		return lightTheme(), true
	case "high-contrast":
		return highContrastTheme(), true
	}
	return Theme{}, false
}

// c is a local shorthand to keep the theme tables readable. lipgloss.Color
// is a `type Color string`, so this is a value-constructor not a function
// reference.
func c(s string) lipgloss.Color { return lipgloss.Color(s) }

func darkTheme() Theme {
	return Theme{
		StatusUp:         lipgloss.NewStyle().Foreground(c("82")),  // green
		StatusDown:       lipgloss.NewStyle().Foreground(c("196")), // red
		StatusConnecting: lipgloss.NewStyle().Foreground(c("214")), // amber
		GroupHeader:      lipgloss.NewStyle().Foreground(c("87")).Bold(true),
		TunnelName:       lipgloss.NewStyle().Foreground(c("231")),
		LatencyGood:      lipgloss.NewStyle().Foreground(c("82")),
		LatencyWarn:      lipgloss.NewStyle().Foreground(c("214")),
		LatencyBad:       lipgloss.NewStyle().Foreground(c("196")),
		HelpText:         lipgloss.NewStyle().Foreground(c("241")),
		ErrorText:        lipgloss.NewStyle().Foreground(c("196")).Bold(true),
		Accent:           lipgloss.NewStyle().Foreground(c("39")),
		Border:           lipgloss.NewStyle().Foreground(c("241")),
	}
}

func lightTheme() Theme {
	return Theme{
		StatusUp:         lipgloss.NewStyle().Foreground(c("28")),
		StatusDown:       lipgloss.NewStyle().Foreground(c("124")),
		StatusConnecting: lipgloss.NewStyle().Foreground(c("130")),
		GroupHeader:      lipgloss.NewStyle().Foreground(c("24")).Bold(true),
		TunnelName:       lipgloss.NewStyle().Foreground(c("232")),
		LatencyGood:      lipgloss.NewStyle().Foreground(c("28")),
		LatencyWarn:      lipgloss.NewStyle().Foreground(c("130")),
		LatencyBad:       lipgloss.NewStyle().Foreground(c("124")),
		HelpText:         lipgloss.NewStyle().Foreground(c("243")),
		ErrorText:        lipgloss.NewStyle().Foreground(c("124")).Bold(true),
		Accent:           lipgloss.NewStyle().Foreground(c("32")),
		Border:           lipgloss.NewStyle().Foreground(c("243")),
	}
}

func highContrastTheme() Theme {
	return Theme{
		StatusUp:         lipgloss.NewStyle().Foreground(c("15")).Background(c("28")).Bold(true),
		StatusDown:       lipgloss.NewStyle().Foreground(c("15")).Background(c("124")).Bold(true),
		StatusConnecting: lipgloss.NewStyle().Foreground(c("15")).Background(c("130")).Bold(true),
		GroupHeader:      lipgloss.NewStyle().Foreground(c("15")).Underline(true).Bold(true),
		TunnelName:       lipgloss.NewStyle().Foreground(c("15")).Bold(true),
		LatencyGood:      lipgloss.NewStyle().Foreground(c("15")).Background(c("28")),
		LatencyWarn:      lipgloss.NewStyle().Foreground(c("15")).Background(c("130")),
		LatencyBad:       lipgloss.NewStyle().Foreground(c("15")).Background(c("124")),
		HelpText:         lipgloss.NewStyle().Foreground(c("15")).Bold(true),
		ErrorText:        lipgloss.NewStyle().Foreground(c("15")).Background(c("196")).Bold(true),
		Accent:           lipgloss.NewStyle().Foreground(c("15")).Underline(true),
		Border:           lipgloss.NewStyle().Foreground(c("15")),
	}
}
