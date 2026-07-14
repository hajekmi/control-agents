package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type historyBenchmarkSpec struct {
	ID      string `json:"id"`
	Lines   int    `json:"lines"`
	Columns int    `json:"columns"`
	ANSI    string `json:"ansi"`
	Text    string `json:"text"`
	Length  string `json:"length"`
	Layout  string `json:"layout"`
}

type historyBenchmarkMetric struct {
	Supported bool   `json:"supported"`
	Value     int64  `json:"value,omitempty"`
	Unit      string `json:"unit,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

type historyBenchmarkDatasetReport struct {
	ID           string                            `json:"id"`
	Axes         historyBenchmarkSpec              `json:"axes"`
	Measurements map[string]historyBenchmarkMetric `json:"measurements"`
}

type historyBenchmarkReport struct {
	SchemaVersion int                             `json:"schemaVersion"`
	Runtime       string                          `json:"runtime"`
	Datasets      []historyBenchmarkDatasetReport `json:"datasets"`
}

func historyBenchmarkSpecs() []historyBenchmarkSpec {
	return []historyBenchmarkSpec{
		{ID: "dataset-01", Lines: 2000, Columns: 80, ANSI: "plain", Text: "ascii", Length: "short", Layout: "shell-log"},
		{ID: "dataset-02", Lines: 10000, Columns: 120, ANSI: "common", Text: "unicode", Length: "short", Layout: "shell-log"},
		{ID: "dataset-03", Lines: 50000, Columns: 240, ANSI: "common", Text: "cjk", Length: "short", Layout: "fixed-grid"},
		{ID: "dataset-04", Lines: 2000, Columns: 120, ANSI: "plain", Text: "emoji", Length: "extreme", Layout: "shell-log"},
		{ID: "dataset-05", Lines: 10000, Columns: 80, ANSI: "common", Text: "ascii", Length: "extreme", Layout: "fixed-grid"},
		{ID: "dataset-06", Lines: 2000, Columns: 80, ANSI: "dense", Text: "cjk", Length: "short", Layout: "fixed-grid"},
	}
}

func TestHistoryBenchmarkDatasetCoversEveryApprovedAxis(t *testing.T) {
	specs := historyBenchmarkSpecs()
	want := map[string][]any{
		"lines":   {2000, 10000, 50000},
		"columns": {80, 120, 240},
		"ansi":    {"plain", "common", "dense"},
		"text":    {"ascii", "unicode", "cjk", "emoji"},
		"length":  {"short", "extreme"},
		"layout":  {"shell-log", "fixed-grid"},
	}
	seen := make(map[string]map[any]bool)
	for axis := range want {
		seen[axis] = make(map[any]bool)
	}
	ids := make(map[string]bool)
	for _, spec := range specs {
		if ids[spec.ID] {
			t.Fatalf("duplicate benchmark dataset ID %q", spec.ID)
		}
		ids[spec.ID] = true
		seen["lines"][spec.Lines] = true
		seen["columns"][spec.Columns] = true
		seen["ansi"][spec.ANSI] = true
		seen["text"][spec.Text] = true
		seen["length"][spec.Length] = true
		seen["layout"][spec.Layout] = true
		preview := spec
		preview.Lines = min(spec.Lines, 4)
		generated := generateHistoryBenchmarkDataset(preview)
		if strings.Count(generated, "\n") != preview.Lines {
			t.Fatalf("dataset %s generated the wrong line count", spec.ID)
		}
	}
	for axis, values := range want {
		for _, value := range values {
			if !seen[axis][value] {
				t.Errorf("benchmark axis %s is missing %v", axis, value)
			}
		}
	}
}

func TestHistoryBenchmarkReport(t *testing.T) {
	output := os.Getenv("CONTROL_AGENTS_BENCHMARK_REPORT")
	if output == "" {
		t.Skip("set CONTROL_AGENTS_BENCHMARK_REPORT to emit the content-free report")
	}
	report := buildHistoryBenchmarkReport(t)
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	encoded = append(encoded, '\n')
	assertContentFreeHistoryBenchmarkReport(t, encoded)
	if err := os.MkdirAll(filepath.Dir(output), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(output, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkHistoryANSIParser(b *testing.B) {
	for _, spec := range historyBenchmarkSpecs() {
		spec := spec
		if spec.Lines > 10000 {
			continue
		}
		b.Run(spec.ID, func(b *testing.B) {
			dataset := generateHistoryBenchmarkDataset(spec)
			b.ReportAllocs()
			b.SetBytes(int64(len(dataset)))
			for range b.N {
				if _, err := parseHistoryANSI(dataset); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func buildHistoryBenchmarkReport(t *testing.T) historyBenchmarkReport {
	t.Helper()
	report := historyBenchmarkReport{SchemaVersion: 1, Runtime: runtime.Version()}
	for _, spec := range historyBenchmarkSpecs() {
		dataset := generateHistoryBenchmarkDataset(spec)
		parseStarted := time.Now()
		lines, err := parseHistoryANSI(dataset)
		parseDuration := time.Since(parseStarted)
		if err != nil {
			t.Fatalf("parse %s: %v", spec.ID, err)
		}
		manager := newSnapshotManager(snapshotManagerConfig{})
		request := testSnapshotCreate("benchmark-login", testHistoryViewer, 0)
		request.Lines = lines
		request.Capture.Before.Columns = spec.Columns
		request.Capture.After.Columns = spec.Columns
		request.Capture.Before.HistorySize = spec.Lines
		request.Capture.After.HistorySize = spec.Lines
		request.NodeEstimate = historyNodeEstimate(lines)
		page, err := manager.Create(request)
		if err != nil {
			t.Fatalf("snapshot %s: %v", spec.ID, err)
		}
		response, err := json.Marshal(page)
		if err != nil {
			t.Fatal(err)
		}
		manager.mu.Lock()
		snapshotRAM := manager.snapshots[page.SnapshotID].Memory
		manager.mu.Unlock()
		report.Datasets = append(report.Datasets, historyBenchmarkDatasetReport{
			ID:   spec.ID,
			Axes: spec,
			Measurements: map[string]historyBenchmarkMetric{
				"ansiParseDuration":   {Supported: true, Value: parseDuration.Nanoseconds(), Unit: "ns"},
				"snapshotRAM":         {Supported: true, Value: snapshotRAM, Unit: "bytes"},
				"responseBytes":       {Supported: true, Value: int64(len(response)), Unit: "bytes"},
				"capturePaneDuration": {Supported: false, Reason: "measured by the real-tmux browser benchmark"},
				"firstHistoryPaint":   {Supported: false, Reason: "requires a browser rendering runtime"},
				"pagePrependDuration": {Supported: false, Reason: "requires a browser rendering runtime"},
				"scrollFPS":           {Supported: false, Reason: "requires a browser rendering runtime"},
				"longTasks":           {Supported: false, Reason: "requires a browser rendering runtime"},
				"domNodeCount":        {Supported: false, Reason: "requires a browser rendering runtime"},
				"jsHeap":              {Supported: false, Reason: "requires a browser rendering runtime"},
				"anchorDrift":         {Supported: false, Reason: "requires a browser rendering runtime"},
				"liveInputToPaint":    {Supported: false, Reason: "requires a Live browser transport"},
				"reconnectToRedraw":   {Supported: false, Reason: "requires a Live browser transport"},
				"slowConsumer":        {Supported: false, Reason: "native bridge queue instrumentation is scheduled for task 0016"},
			},
		})
	}
	return report
}

func generateHistoryBenchmarkDataset(spec historyBenchmarkSpec) string {
	unit := map[string]string{
		"ascii":   "alpha-0123456789",
		"unicode": "résumé-λ-e\u0301",
		"cjk":     "日本語-한글-中文",
		"emoji":   "👩🏽‍💻-🧑‍🚀-🙂",
	}[spec.Text]
	if unit == "" {
		panic("unknown benchmark text axis")
	}
	var output strings.Builder
	visibleLineBytes := spec.Columns
	if spec.Length == "extreme" {
		visibleLineBytes = historyMaxLineBytes / 4
	}
	for line := 0; line < spec.Lines; line++ {
		prefix := fmt.Sprintf("%06d|", line)
		bodyBytes := max(spec.Columns-len(prefix), len(unit))
		if spec.Length == "extreme" && line == spec.Lines/2 {
			bodyBytes = visibleLineBytes
		}
		body := repeatToBytes(unit, bodyBytes)
		if spec.Layout == "shell-log" {
			body = prefix + body
		}
		switch spec.ANSI {
		case "plain":
			output.WriteString(body)
		case "common":
			output.WriteString("\x1b[1;38;5;67m")
			output.WriteString(body)
			output.WriteString("\x1b[0m")
		case "dense":
			for index, value := range body {
				if index%2 == 0 {
					output.WriteString("\x1b[31m")
				} else {
					output.WriteString("\x1b[32m")
				}
				output.WriteRune(value)
			}
			output.WriteString("\x1b[0m")
		default:
			panic("unknown benchmark ANSI axis")
		}
		output.WriteByte('\n')
	}
	return output.String()
}

func repeatToBytes(unit string, target int) string {
	var output strings.Builder
	for output.Len() < target {
		output.WriteString(unit)
	}
	return output.String()
}

func assertContentFreeHistoryBenchmarkReport(t *testing.T, encoded []byte) {
	t.Helper()
	if len(encoded) > 128*1024 {
		t.Fatalf("benchmark report is unbounded: %d bytes", len(encoded))
	}
	text := string(encoded)
	for _, forbidden := range []string{"CONTROL_AGENTS_PASSWORD", "control_agents_session", "playwright-line-", "<script", "SSH_AUTH_SOCK", "pt_", "hs_", "viewer-"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("benchmark report contains forbidden content marker %q", forbidden)
		}
	}
	var decoded historyBenchmarkReport
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.SchemaVersion != 1 || decoded.Runtime == "" || len(decoded.Datasets) != len(historyBenchmarkSpecs()) {
		t.Fatalf("benchmark report structure = %#v", decoded)
	}
}
