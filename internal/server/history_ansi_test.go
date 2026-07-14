package server

import (
	"context"
	"errors"
	"strings"
	"testing"

	"control-agents/internal/tmux"
)

func TestHistoryANSIParsesStylesAndDropsActiveControls(t *testing.T) {
	input := "<script>alert(1)</script> " +
		"\x1b[1;3;4;9;38;5;196;48;2;1;2;3mstyled\x1b[0m" +
		"\x1b]8;;https://evil.example\x07link\x1b]8;;\x1b\\" +
		"\x1bPignored-dcs\x1b\\\x1b_apc\x1b\\\x1b^pm\x1b\\"
	lines, err := parseHistoryANSI(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || len(lines[0].Runs) != 3 {
		t.Fatalf("lines = %#v", lines)
	}
	if got := lines[0].Runs[0].Text; got != "<script>alert(1)</script> " {
		t.Fatalf("plain text = %q", got)
	}
	style := lines[0].Runs[1].Style
	if style == nil || !style.Bold || !style.Italic || !style.Underline || !style.Strike || style.Foreground != "#ff0000" || style.Background != "#010203" {
		t.Fatalf("style = %#v", style)
	}
	if got := lines[0].Runs[2].Text; got != "link" {
		t.Fatalf("OSC/DCS stripping left %q", got)
	}
}

func TestHistoryANSICoalescesRunsAndHandlesIncompleteEscape(t *testing.T) {
	lines, err := parseHistoryANSI("\x1b[31mred\x1b[31m again\nplain\x1b[38;2;255")
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 || len(lines[0].Runs) != 1 || lines[0].Runs[0].Text != "red again" {
		t.Fatalf("coalesced lines = %#v", lines)
	}
	if lines[0].Runs[0].Style == nil || lines[0].Runs[0].Style.Foreground != "#cd0000" {
		t.Fatalf("red style = %#v", lines[0].Runs[0].Style)
	}
	if len(lines[1].Runs) != 1 || lines[1].Runs[0].Text != "plain" {
		t.Fatalf("incomplete escape result = %#v", lines[1])
	}
}

func TestHistoryANSICoalescesAdversarialSegmentsLinearly(t *testing.T) {
	const segments = 100000
	var input strings.Builder
	input.Grow(segments * 6)
	for range segments {
		input.WriteString("\x1b[31mx")
	}

	lines, err := parseHistoryANSI(input.String())
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || len(lines[0].Runs) != 1 || len(lines[0].Runs[0].Text) != segments {
		t.Fatalf("coalesced adversarial history = %#v", lines)
	}
	if style := lines[0].Runs[0].Style; style == nil || style.Foreground != "#cd0000" {
		t.Fatalf("coalesced adversarial style = %#v", style)
	}
}

func TestHistoryANSIParsesColonTruecolorAndEightBitCSI(t *testing.T) {
	lines, err := parseHistoryANSI("\x1b[38:2::12:34:56mcolon\x9b48:5:196meight-bit")
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || len(lines[0].Runs) != 2 {
		t.Fatalf("lines = %#v", lines)
	}
	if style := lines[0].Runs[0].Style; style == nil || style.Foreground != "#0c2238" {
		t.Fatalf("colon style = %#v", style)
	}
	if style := lines[0].Runs[1].Style; style == nil || style.Foreground != "#0c2238" || style.Background != "#ff0000" {
		t.Fatalf("eight-bit style = %#v", style)
	}
}

func TestHistoryANSIParsesSixteenIndexedAndTruecolorPalette(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		foreground string
		background string
	}{
		{name: "base sixteen", input: "\x1b[31;44mvalue", foreground: "#cd0000", background: "#0000ee"},
		{name: "bright sixteen", input: "\x1b[96;101mvalue", foreground: "#00ffff", background: "#ff0000"},
		{name: "indexed cube", input: "\x1b[38;5;67;48;5;231mvalue", foreground: "#5f87af", background: "#ffffff"},
		{name: "indexed grayscale", input: "\x1b[38;5;244;48;5;232mvalue", foreground: "#808080", background: "#080808"},
		{name: "truecolor", input: "\x1b[38;2;12;34;56;48;2;210;220;230mvalue", foreground: "#0c2238", background: "#d2dce6"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lines, err := parseHistoryANSI(test.input)
			if err != nil {
				t.Fatal(err)
			}
			if len(lines) != 1 || len(lines[0].Runs) != 1 || lines[0].Runs[0].Text != "value" {
				t.Fatalf("parsed lines = %#v", lines)
			}
			style := lines[0].Runs[0].Style
			if style == nil || style.Foreground != test.foreground || style.Background != test.background {
				t.Fatalf("style = %#v, want foreground %q and background %q", style, test.foreground, test.background)
			}
		})
	}
}

func TestHistoryANSIResetsWholeAndPartialStyles(t *testing.T) {
	input := "\x1b[1;2;3;4;7;9;31;44mall" +
		"\x1b[22;23;24;27;29;39;49mpartial" +
		"\x1b[1;32mgreen\x1b[0mplain"
	lines, err := parseHistoryANSI(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || len(lines[0].Runs) != 4 {
		t.Fatalf("runs = %#v", lines)
	}
	all := lines[0].Runs[0].Style
	if all == nil || !all.Bold || !all.Faint || !all.Italic || !all.Underline || !all.Inverse || !all.Strike {
		t.Fatalf("all attributes = %#v", all)
	}
	if style := lines[0].Runs[1].Style; style != nil {
		t.Fatalf("partial reset style = %#v, want plain", style)
	}
	if style := lines[0].Runs[2].Style; style == nil || !style.Bold || style.Foreground != "#00cd00" {
		t.Fatalf("style before full reset = %#v", style)
	}
	if style := lines[0].Runs[3].Style; style != nil {
		t.Fatalf("full reset style = %#v, want plain", style)
	}
}

func TestHistoryANSIHandlesEveryIncompleteSequenceBoundary(t *testing.T) {
	sequences := []string{
		"prefix\x1b[38;2;12;34;56msuffix",
		"prefix\x1b]52;c;clipboard\x07suffix",
		"prefix\x1bPprivate-data\x1b\\suffix",
	}
	for _, complete := range sequences {
		for boundary := len("prefix"); boundary < len(complete); boundary++ {
			if _, err := parseHistoryANSI(complete[:boundary]); err != nil {
				t.Fatalf("boundary %d of %q returned %v", boundary, complete, err)
			}
		}
	}
}

func TestHistoryANSIPreservesUnicodeGraphemeBytesAsText(t *testing.T) {
	input := "Latin e\u0301 | 日本語 | 한글 | 👩🏽‍💻 | 🧑‍🚀"
	lines, err := parseHistoryANSI(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || len(lines[0].Runs) != 1 || lines[0].Runs[0].Text != input {
		t.Fatalf("Unicode history = %#v", lines)
	}
}

func TestHistoryANSIKeepsHTMLPayloadInInertStructuredText(t *testing.T) {
	input := `<img src=x onerror="globalThis.pwned=true"><script>alert(1)</script>&quot;`
	lines, err := parseHistoryANSI(input + "\x1b]52;c;PHNjcmlwdD4=\x07")
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || len(lines[0].Runs) != 1 || lines[0].Runs[0].Text != input {
		t.Fatalf("structured XSS text = %#v", lines)
	}
}

func TestHistoryANSIEnforcesLineLimit(t *testing.T) {
	_, err := parseHistoryANSI(strings.Repeat("x", historyMaxLineBytes+1))
	if !errors.Is(err, errHistoryANSIResourceLimit) {
		t.Fatalf("error = %v, want resource limit", err)
	}
}

func TestHistoryANSIEnforcesRunLimit(t *testing.T) {
	var input strings.Builder
	for index := 0; index <= historyMaxRunsPerLine; index++ {
		if index%2 == 0 {
			input.WriteString("\x1b[31mx")
		} else {
			input.WriteString("\x1b[32mx")
		}
	}
	if _, err := parseHistoryANSI(input.String()); !errors.Is(err, errHistoryANSIResourceLimit) {
		t.Fatalf("error = %v, want run resource limit", err)
	}
}

func TestHistoryANSIEnforcesAggregateSnapshotRunLimit(t *testing.T) {
	var input strings.Builder
	for index := 0; index <= historyMaxRunsPerSnapshot; index++ {
		if index > 0 && index%historyMaxRunsPerLine == 0 {
			input.WriteByte('\n')
		}
		if index%2 == 0 {
			input.WriteString("\x1b[31mx")
		} else {
			input.WriteString("\x1b[32mx")
		}
	}
	if _, err := parseHistoryANSI(input.String()); !errors.Is(err, errHistoryANSIResourceLimit) {
		t.Fatalf("error = %v, want aggregate run resource limit", err)
	}
}

func TestHistoryANSIEnforcesStructuredMemoryWhileParsing(t *testing.T) {
	limits := testHistoryANSIParseLimits()
	limits.MaxStructuredBytes = 300

	if _, err := parseHistoryANSIWithLimits(strings.Repeat("a", 80)+"\n"+strings.Repeat("b", 80), limits); !errors.Is(err, errHistoryANSIResourceLimit) {
		t.Fatalf("error = %v, want structured-memory resource limit", err)
	}
}

func TestHistoryANSIRejectsNewlineHeavyCaptureBeforeLineMaterialization(t *testing.T) {
	input := strings.Repeat("\n", tmux.DefaultSnapshotBytes-1)
	if _, err := parseHistoryANSI(input); !errors.Is(err, errHistoryANSIResourceLimit) {
		t.Fatalf("error = %v, want aggregate line resource limit", err)
	}
}

func TestHistoryANSIDropsEightBitStringControls(t *testing.T) {
	input := "safe\x90dcs\x9c\x9dosc\x07\x9eignored\x9c\x9fignored\x9cend"
	lines, err := parseHistoryANSI(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || len(lines[0].Runs) != 1 || lines[0].Runs[0].Text != "safeend" {
		t.Fatalf("control strings survived: %#v", lines)
	}
}

func TestHistoryANSIHonorsCancellationAndBoundsSGRParsing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := parseHistoryANSIContext(ctx, strings.Repeat("safe", 4096)); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled parse error = %v", err)
	}

	input := "\x1b[" + strings.Repeat("1;", historyMaxSGRBytes) + "31mplain"
	lines, err := parseHistoryANSI(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || len(lines[0].Runs) != 1 || lines[0].Runs[0].Text != "plain" || lines[0].Runs[0].Style != nil {
		t.Fatalf("oversized SGR was not safely ignored: %#v", lines)
	}
}

func TestHistoryANSIReplacesBidiControlsWithVisibleCopyWarning(t *testing.T) {
	lines, err := parseHistoryANSI("safe\u202eevil\u2066end")
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || !lines[0].BidiWarning || len(lines[0].Runs) != 1 {
		t.Fatalf("bidi warning line = %#v", lines)
	}
	if got := lines[0].Runs[0].Text; got != "safe[BIDI U+202E]evil[BIDI U+2066]end" {
		t.Fatalf("visible bidi text = %q", got)
	}
	if strings.ContainsRune(lines[0].Runs[0].Text, '\u202e') || strings.ContainsRune(lines[0].Runs[0].Text, '\u2066') {
		t.Fatal("invisible bidi control survived structured output")
	}
}

func testHistoryANSIParseLimits() historyANSIParseLimits {
	return historyANSIParseLimits{
		MaxLineBytes:       historyMaxLineBytes,
		MaxLines:           historyMaxLinesPerSnapshot,
		MaxRunsPerLine:     historyMaxRunsPerLine,
		MaxRunsPerSnapshot: historyMaxRunsPerSnapshot,
		MaxStructuredBytes: historyMaxStructuredBytes,
	}
}
