package command

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestWriteEntriesText(t *testing.T) {
	t.Parallel()

	entries := []json.RawMessage{
		json.RawMessage(`{"Created":"2026-09-10T07:00:00Z","Message":"Configuration job completed"}`),
	}
	var output bytes.Buffer
	if err := writeEntries(&output, entries, "text"); err != nil {
		t.Fatal(err)
	}
	if want := "2026-09-10T07:00:00Z Configuration job completed\n"; output.String() != want {
		t.Fatalf("output = %q, want %q", output.String(), want)
	}
}

func TestMissingMasterIsReported(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	exitCode := Run([]string{"lc-logs", "10.46.96.160"}, &stdout, &stderr, func(string) string { return "" })
	if exitCode != 1 {
		t.Fatalf("exit code = %d, want 1", exitCode)
	}
	if !strings.Contains(stderr.String(), "BMC_MASTER must be set") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestHelp(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	if exitCode := Run([]string{"help"}, &stdout, &stderr, func(string) string { return "" }); exitCode != 0 {
		t.Fatalf("exit code = %d, want 0", exitCode)
	}
	if !strings.Contains(stdout.String(), "lc-logs") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}
