package tui

import "testing"

func TestSanitise_stripsAnsiEscape(t *testing.T) {
	out := sanitiseForTerminal("hello \033[31mred\033[0m world")
	want := "hello [31mred[0m world"
	if out != want {
		t.Errorf("want %q, got %q", want, out)
	}
}

func TestSanitise_preservesNewlineTab(t *testing.T) {
	out := sanitiseForTerminal("a\tb\nc")
	if out != "a\tb\nc" {
		t.Errorf("want %q, got %q", "a\tb\nc", out)
	}
}

func TestSanitise_dropsOscHyperlink(t *testing.T) {
	out := sanitiseForTerminal("\033]8;;http://evil\033\\click me\033]8;;\033\\")
	// \033 (ESC) is stripped; the remainder is rendered literally.
	if want := "]8;;http://evil\\click me]8;;\\"; out != want {
		t.Errorf("want %q, got %q", want, out)
	}
}

func TestSanitise_emptyAndAllControl(t *testing.T) {
	if sanitiseForTerminal("") != "" {
		t.Error("empty input should yield empty")
	}
	if sanitiseForTerminal("\x00\x01\x02\x03") != "" {
		t.Error("all-control input should yield empty")
	}
}
