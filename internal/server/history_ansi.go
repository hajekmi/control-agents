package server

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	historyMaxLineBytes           = 1024 * 1024
	historyMaxLinesPerSnapshot    = 100000
	historyMaxRunsPerLine         = 4096
	historyMaxRunsPerSnapshot     = 200000
	historyMaxStructuredBytes     = 128 * 1024 * 1024
	historyMaxSGRBytes            = 4096
	historyStructuredLineOverhead = 32
	historyStructuredRunOverhead  = 96
)

var errHistoryANSIResourceLimit = errors.New("terminal history ANSI resource limit exceeded")

type historyStyle struct {
	Foreground string `json:"foreground,omitempty"`
	Background string `json:"background,omitempty"`
	Bold       bool   `json:"bold,omitempty"`
	Faint      bool   `json:"faint,omitempty"`
	Italic     bool   `json:"italic,omitempty"`
	Underline  bool   `json:"underline,omitempty"`
	Inverse    bool   `json:"inverse,omitempty"`
	Strike     bool   `json:"strike,omitempty"`
}

type historyRun struct {
	Text  string        `json:"text"`
	Style *historyStyle `json:"style,omitempty"`
}

type historyLine struct {
	Runs        []historyRun `json:"runs"`
	BidiWarning bool         `json:"bidiWarning,omitempty"`
}

type historyRunBuilder struct {
	text  strings.Builder
	style *historyStyle
}

func parseHistoryANSI(input string) ([]historyLine, error) {
	return parseHistoryANSIContext(context.Background(), input)
}

func parseHistoryANSIContext(ctx context.Context, input string) ([]historyLine, error) {
	return parseHistoryANSIWithLimitsContext(ctx, input, historyANSIParseLimits{
		MaxLineBytes:       historyMaxLineBytes,
		MaxLines:           historyMaxLinesPerSnapshot,
		MaxRunsPerLine:     historyMaxRunsPerLine,
		MaxRunsPerSnapshot: historyMaxRunsPerSnapshot,
		MaxStructuredBytes: historyMaxStructuredBytes,
	})
}

type historyANSIParseLimits struct {
	MaxLineBytes       int
	MaxLines           int
	MaxRunsPerLine     int
	MaxRunsPerSnapshot int
	MaxStructuredBytes int64
}

func parseHistoryANSIWithLimits(input string, limits historyANSIParseLimits) ([]historyLine, error) {
	return parseHistoryANSIWithLimitsContext(context.Background(), input, limits)
}

func parseHistoryANSIWithLimitsContext(ctx context.Context, input string, limits historyANSIParseLimits) ([]historyLine, error) {
	if limits.MaxLineBytes <= 0 || limits.MaxLines <= 0 || limits.MaxRunsPerLine <= 0 || limits.MaxRunsPerSnapshot <= 0 || limits.MaxStructuredBytes <= 0 {
		return nil, errHistoryANSIResourceLimit
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	lines := make([]historyLine, 0, min(256, limits.MaxLines))
	runs := make([]*historyRunBuilder, 0, min(8, limits.MaxRunsPerLine))
	style := historyStyle{}
	lineBytes := 0
	lineBidiWarning := false
	totalRuns := 0
	var structuredBytes int64

	reserveStructured := func(bytes int64) error {
		if bytes < 0 || bytes > limits.MaxStructuredBytes-structuredBytes {
			return errHistoryANSIResourceLimit
		}
		structuredBytes += bytes
		return nil
	}
	flushLine := func() error {
		if len(lines) >= limits.MaxLines {
			return errHistoryANSIResourceLimit
		}
		if err := reserveStructured(historyStructuredLineOverhead); err != nil {
			return err
		}
		lineRuns := make([]historyRun, len(runs))
		for index := range runs {
			lineRuns[index] = historyRun{Text: runs[index].text.String(), Style: runs[index].style}
		}
		lines = append(lines, historyLine{Runs: lineRuns, BidiWarning: lineBidiWarning})
		runs = make([]*historyRunBuilder, 0, min(8, limits.MaxRunsPerLine))
		lineBytes = 0
		lineBidiWarning = false
		return nil
	}
	appendText := func(value string) error {
		if len(value) > limits.MaxLineBytes-lineBytes {
			return errHistoryANSIResourceLimit
		}
		if err := reserveStructured(int64(len(value))); err != nil {
			return errHistoryANSIResourceLimit
		}
		lineBytes += len(value)
		if len(runs) == 0 || !equalHistoryStyle(runs[len(runs)-1].style, style) {
			if len(runs) >= limits.MaxRunsPerLine || totalRuns >= limits.MaxRunsPerSnapshot {
				return errHistoryANSIResourceLimit
			}
			if err := reserveStructured(historyStructuredRunOverhead); err != nil {
				return err
			}
			run := &historyRunBuilder{}
			if style != (historyStyle{}) {
				copy := style
				run.style = &copy
			}
			runs = append(runs, run)
			totalRuns++
		}
		runs[len(runs)-1].text.WriteString(value)
		return nil
	}

	for index := 0; index < len(input); {
		if index%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		value := input[index]
		if value >= utf8.RuneSelf {
			runeValue, size := utf8.DecodeRuneInString(input[index:])
			if runeValue == utf8.RuneError && size == 1 && value >= 0x90 && value <= 0x9f {
				switch value {
				case 0x90, 0x9d, 0x9e, 0x9f:
					var err error
					index, err = consumeStringControl(ctx, input, index+1, value == 0x9d)
					if err != nil {
						return nil, err
					}
				case 0x9b:
					next, sgr, params, err := consumeCSI(ctx, input, index+1)
					if err != nil {
						return nil, err
					}
					if sgr && len(params) <= historyMaxSGRBytes {
						applySGR(&style, params)
					}
					index = next
				default:
					index++
				}
				continue
			}
			if label, bidi := bidiControlLabel(runeValue); bidi {
				if err := appendText(label); err != nil {
					return nil, err
				}
				lineBidiWarning = true
				index += size
				continue
			}
			if err := appendText(input[index : index+size]); err != nil {
				return nil, err
			}
			index += size
			continue
		}
		switch value {
		case '\n':
			if err := flushLine(); err != nil {
				return nil, err
			}
			index++
		case '\r':
			index++
		case '\t':
			if err := appendText("\t"); err != nil {
				return nil, err
			}
			index++
		case 0x1b:
			next, sgr, params, err := consumeEscapeSequence(ctx, input, index)
			if err != nil {
				return nil, err
			}
			if sgr && len(params) <= historyMaxSGRBytes {
				applySGR(&style, params)
			}
			index = next
		default:
			if value < 0x20 || value == 0x7f {
				index++
				continue
			}
			start := index
			for index < len(input) {
				if (index-start)%4096 == 0 {
					if err := ctx.Err(); err != nil {
						return nil, err
					}
				}
				candidate := input[index]
				if candidate >= utf8.RuneSelf || candidate == '\n' || candidate == '\r' || candidate == '\t' || candidate == 0x1b || candidate < 0x20 || candidate == 0x7f {
					break
				}
				index++
			}
			if err := appendText(input[start:index]); err != nil {
				return nil, err
			}
		}
	}
	if err := flushLine(); err != nil {
		return nil, err
	}
	return lines, nil
}

func bidiControlLabel(value rune) (string, bool) {
	switch {
	case value == '\u061c', value == '\u200e', value == '\u200f',
		value >= '\u202a' && value <= '\u202e', value >= '\u2066' && value <= '\u2069':
		return fmt.Sprintf("[BIDI U+%04X]", value), true
	default:
		return "", false
	}
}

func equalHistoryStyle(existing *historyStyle, current historyStyle) bool {
	if existing == nil {
		return current == (historyStyle{})
	}
	return *existing == current
}

func consumeEscapeSequence(ctx context.Context, input string, start int) (next int, sgr bool, params string, err error) {
	if start+1 >= len(input) {
		return len(input), false, "", nil
	}
	switch input[start+1] {
	case '[':
		return consumeCSI(ctx, input, start+2)
	case ']', 'P', '_', '^':
		next, err := consumeStringControl(ctx, input, start+2, input[start+1] == ']')
		return next, false, "", err
	default:
		return min(len(input), start+2), false, "", nil
	}
}

func consumeCSI(ctx context.Context, input string, paramsStart int) (next int, sgr bool, params string, err error) {
	for index := paramsStart; index < len(input); index++ {
		if (index-paramsStart)%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return 0, false, "", err
			}
		}
		if input[index] >= 0x40 && input[index] <= 0x7e {
			return index + 1, input[index] == 'm', input[paramsStart:index], nil
		}
	}
	return len(input), false, "", nil
}

func consumeStringControl(ctx context.Context, input string, start int, allowBEL bool) (int, error) {
	for index := start; index < len(input); index++ {
		if (index-start)%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return 0, err
			}
		}
		if allowBEL && input[index] == 0x07 {
			return index + 1, nil
		}
		if input[index] == 0x1b && index+1 < len(input) && input[index+1] == '\\' {
			return index + 2, nil
		}
		if input[index] == 0x9c {
			return index + 1, nil
		}
	}
	return len(input), nil
}

func applySGR(style *historyStyle, raw string) {
	raw = normalizeSGRSeparators(raw)
	if raw == "" {
		raw = "0"
	}
	parts := strings.Split(raw, ";")
	values := make([]int, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			values = append(values, 0)
			continue
		}
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 {
			return
		}
		values = append(values, value)
	}
	for index := 0; index < len(values); index++ {
		code := values[index]
		switch {
		case code == 0:
			*style = historyStyle{}
		case code == 1:
			style.Bold = true
		case code == 2:
			style.Faint = true
		case code == 3:
			style.Italic = true
		case code == 4 || code == 21:
			style.Underline = true
		case code == 7:
			style.Inverse = true
		case code == 9:
			style.Strike = true
		case code == 22:
			style.Bold = false
			style.Faint = false
		case code == 23:
			style.Italic = false
		case code == 24:
			style.Underline = false
		case code == 27:
			style.Inverse = false
		case code == 29:
			style.Strike = false
		case code == 39:
			style.Foreground = ""
		case code == 49:
			style.Background = ""
		case code >= 30 && code <= 37:
			style.Foreground = ansiColor(code - 30)
		case code >= 40 && code <= 47:
			style.Background = ansiColor(code - 40)
		case code >= 90 && code <= 97:
			style.Foreground = ansiColor(code - 90 + 8)
		case code >= 100 && code <= 107:
			style.Background = ansiColor(code - 100 + 8)
		case code == 38 || code == 48:
			color, consumed, ok := parseExtendedColor(values[index+1:])
			if !ok {
				continue
			}
			if code == 38 {
				style.Foreground = color
			} else {
				style.Background = color
			}
			index += consumed
		}
	}
}

func normalizeSGRSeparators(raw string) string {
	groups := strings.Split(raw, ";")
	normalized := make([]string, 0, len(groups)+4)
	for _, group := range groups {
		parts := strings.Split(group, ":")
		if len(parts) == 1 {
			normalized = append(normalized, group)
			continue
		}
		if (parts[0] == "38" || parts[0] == "48") && len(parts) >= 3 && (parts[1] == "2" || parts[1] == "5") {
			normalized = append(normalized, parts[0], parts[1])
			start := 2
			if parts[1] == "2" && len(parts) >= 6 {
				// ISO-8613-6 permits an optional color-space identifier here.
				start = 3
			}
			normalized = append(normalized, parts[start:]...)
			continue
		}
		// Unsupported SGR subparameters retain only their allowlisted base code.
		normalized = append(normalized, parts[0])
	}
	return strings.Join(normalized, ";")
}

func parseExtendedColor(values []int) (string, int, bool) {
	if len(values) >= 2 && values[0] == 5 && values[1] >= 0 && values[1] <= 255 {
		return ansiColor(values[1]), 2, true
	}
	if len(values) >= 4 && values[0] == 2 && validRGB(values[1], values[2], values[3]) {
		return rgbColor(values[1], values[2], values[3]), 4, true
	}
	return "", 0, false
}

func validRGB(red, green, blue int) bool {
	return red >= 0 && red <= 255 && green >= 0 && green <= 255 && blue >= 0 && blue <= 255
}

func rgbColor(red, green, blue int) string {
	return fmt.Sprintf("#%02x%02x%02x", red, green, blue)
}

func ansiColor(index int) string {
	base := [16]string{
		"#000000", "#cd0000", "#00cd00", "#cdcd00", "#0000ee", "#cd00cd", "#00cdcd", "#e5e5e5",
		"#7f7f7f", "#ff0000", "#00ff00", "#ffff00", "#5c5cff", "#ff00ff", "#00ffff", "#ffffff",
	}
	if index >= 0 && index < len(base) {
		return base[index]
	}
	if index >= 16 && index <= 231 {
		value := index - 16
		levels := [6]int{0, 95, 135, 175, 215, 255}
		return rgbColor(levels[value/36], levels[value/6%6], levels[value%6])
	}
	if index >= 232 && index <= 255 {
		level := 8 + (index-232)*10
		return rgbColor(level, level, level)
	}
	return ""
}
