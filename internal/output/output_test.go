package output_test

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/frknue/shopify-tools/internal/output"
)

type sample struct {
	Name  string `json:"name" yaml:"name"`
	Count int    `json:"count" yaml:"count"`
}

func (s sample) Headers() []string { return []string{"NAME", "COUNT"} }
func (s sample) Rows() [][]string  { return [][]string{{s.Name, strconv.Itoa(s.Count)}} }

func TestParseFormat(t *testing.T) {
	t.Parallel()

	for _, in := range []string{"table", "JSON", " yaml "} {
		if _, err := output.ParseFormat(in); err != nil {
			t.Errorf("ParseFormat(%q) returned error: %v", in, err)
		}
	}
	if _, err := output.ParseFormat("xml"); err == nil {
		t.Error("ParseFormat(xml) = nil error, want error")
	}
}

func TestPrintTable(t *testing.T) {
	var buf bytes.Buffer
	if err := output.New(&buf, output.FormatTable).Print(sample{Name: "widget", Count: 1}); err != nil {
		t.Fatalf("Print() returned error: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "NAME") || !strings.Contains(got, "widget") {
		t.Errorf("table output missing header or row:\n%s", got)
	}
}

func TestPrintJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := output.New(&buf, output.FormatJSON).Print(sample{Name: "widget", Count: 1}); err != nil {
		t.Fatalf("Print() returned error: %v", err)
	}
	var got sample
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("JSON output is invalid: %v\n%s", err, buf.String())
	}
	if got.Name != "widget" || got.Count != 1 {
		t.Errorf("decoded = %+v, want {widget 1}", got)
	}
}

// A value that is not a Tabler must still render in table mode.
func TestPrintTableFallsBackToJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := output.New(&buf, output.FormatTable).Print(map[string]int{"a": 1}); err != nil {
		t.Fatalf("Print() returned error: %v", err)
	}
	if !strings.Contains(buf.String(), `"a": 1`) {
		t.Errorf("non-Tabler value did not fall back to JSON:\n%s", buf.String())
	}
}

func TestPrintYAML(t *testing.T) {
	var buf bytes.Buffer
	if err := output.New(&buf, output.FormatYAML).Print(sample{Name: "widget", Count: 1}); err != nil {
		t.Fatalf("Print() returned error: %v", err)
	}
	if !strings.Contains(buf.String(), "name: widget") {
		t.Errorf("YAML output unexpected:\n%s", buf.String())
	}
}
