package command

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestProgressDescription(t *testing.T) {
	t.Parallel()

	tests := []struct {
		args []string
		want string
	}{
		{args: []string{"10.46.96.160"}, want: "Querying system summary..."},
		{args: []string{"--verify-tls", "inventory", "10.46.96.160"}, want: "Collecting hardware inventory..."},
		{args: []string{"settings", "bios", "--set", "SecureBoot=Disabled", "10.46.96.160"}, want: "Updating iDRAC settings..."},
		{args: []string{"status", "--monitor", "10.46.96.160"}, want: "Querying system status..."},
		{args: []string{"query", "--help"}, want: ""},
		{args: []string{"unknown", "10.46.96.160"}, want: ""},
	}
	for _, test := range tests {
		if got := progressDescription(test.args); got != test.want {
			t.Errorf("progressDescription(%q) = %q, want %q", test.args, got, test.want)
		}
	}
}

func TestProgressStopsBeforeWritingCommandOutput(t *testing.T) {
	t.Parallel()

	var terminal bytes.Buffer
	progress := newTerminalProgress(&terminal, "Waiting...")
	output := progress.StopOnWrite(&terminal)
	if _, err := output.Write([]byte("result\n")); err != nil {
		t.Fatal(err)
	}
	progress.Stop()

	got := terminal.String()
	if !strings.HasPrefix(got, "\r| Waiting...") {
		t.Fatalf("output did not start with spinner: %q", got)
	}
	if !strings.Contains(got, "\r\x1b[2Kresult\n") {
		t.Fatalf("spinner was not cleared before command output: %q", got)
	}
}

func TestProgressShowsServerRetryDelay(t *testing.T) {
	t.Parallel()

	var terminal bytes.Buffer
	progress := newTerminalProgress(&terminal, "Waiting...")
	progress.Retrying(30 * time.Second)
	progress.Stop()

	if !strings.Contains(terminal.String(), "iDRAC unavailable; retrying in 30s...") {
		t.Fatalf("retry notice missing from spinner output: %q", terminal.String())
	}
}
