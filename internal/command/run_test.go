package command

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stevekeay/dracli/internal/redfish"
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

func TestWriteStatusShowsSinceOnlyWithoutHistory(t *testing.T) {
	t.Parallel()

	status := redfish.SystemStatus{
		PowerState: "On",
		BootProgress: redfish.BootProgress{
			LastState:     "OSRunning",
			LastStateTime: "2026-09-10T07:00:00Z",
		},
	}
	observedAt := time.Date(2026, time.September, 11, 8, 30, 0, 0, time.UTC)

	var initial bytes.Buffer
	if err := writeStatus(&initial, status, observedAt, "text", true, false); err != nil {
		t.Fatal(err)
	}
	if want := "2026-09-11T08:30:00Z power=On boot=OSRunning since=2026-09-10T07:00:00Z\n"; initial.String() != want {
		t.Fatalf("initial output = %q, want %q", initial.String(), want)
	}

	var changed bytes.Buffer
	if err := writeStatus(&changed, status, observedAt, "text", true, true); err != nil {
		t.Fatal(err)
	}
	if want := "2026-09-11T08:30:00Z power=On boot=OSRunning\n"; changed.String() != want {
		t.Fatalf("changed output = %q, want %q", changed.String(), want)
	}
}

func TestMissingMasterIsReported(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	exitCode := Run([]string{"logs", "10.46.96.160"}, &stdout, &stderr, func(string) string { return "" })
	if exitCode != 1 {
		t.Fatalf("exit code = %d, want 1", exitCode)
	}
	if !strings.Contains(stderr.String(), "BMC_MASTER must be set") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestSystemEventLogsCommandIsRecognized(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	exitCode := Run([]string{"sel-logs", "10.46.96.160"}, &stdout, &stderr, func(string) string { return "" })
	if exitCode != 1 {
		t.Fatalf("exit code = %d, want 1", exitCode)
	}
	if !strings.Contains(stderr.String(), "BMC_MASTER must be set") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestLogCommandHelp(t *testing.T) {
	t.Parallel()

	for _, command := range []string{"logs", "sel-logs"} {
		var stdout, stderr bytes.Buffer
		exitCode := Run([]string{command, "--help"}, &stdout, &stderr, func(string) string { return "" })
		if exitCode != 0 {
			t.Errorf("%s: exit code = %d, stderr = %q", command, exitCode, stderr.String())
		}
		if !strings.Contains(stdout.String(), "Usage: dracli "+command) {
			t.Errorf("%s: stdout = %q", command, stdout.String())
		}
	}
}

func TestHelp(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	if exitCode := Run([]string{"help"}, &stdout, &stderr, func(string) string { return "" }); exitCode != 0 {
		t.Fatalf("exit code = %d, want 0", exitCode)
	}
	if !strings.Contains(stdout.String(), "Global options:") ||
		!strings.Contains(stdout.String(), "settings bios") ||
		!strings.Contains(stdout.String(), "sel-logs") ||
		!strings.Contains(stdout.String(), "Password precedence") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestHelpFlagAliasesHelpCommand(t *testing.T) {
	t.Parallel()

	var helpOutput bytes.Buffer
	if exitCode := Run([]string{"--help"}, &helpOutput, io.Discard, func(string) string { return "" }); exitCode != 0 {
		t.Fatalf("exit code = %d, want 0", exitCode)
	}
	var commandOutput bytes.Buffer
	if exitCode := Run([]string{"help"}, &commandOutput, io.Discard, func(string) string { return "" }); exitCode != 0 {
		t.Fatalf("exit code = %d, want 0", exitCode)
	}
	if helpOutput.String() != commandOutput.String() {
		t.Fatal("--help and help produced different output")
	}
}

func TestCompletionScripts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		shell string
		want  []string
	}{
		{
			shell: "bash",
			want: []string{
				"complete -F _dracli dracli",
				"completion logs sel-logs query inventory status jobs clear-jobs factory-reset settings help",
				"--manager --system --all --name --set",
				"drac bios",
				"text json",
			},
		},
		{
			shell: "zsh",
			want: []string{
				"#compdef dracli",
				"compdef _dracli dracli",
				"'factory-reset:Reset iDRAC settings to factory defaults'",
				"'--set:Set NAME=VALUE (repeatable)'",
				"'settings namespace' drac bios",
				"'output format' text json",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.shell, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			exitCode := Run([]string{"completion", test.shell}, &stdout, &stderr, func(string) string { return "" })
			if exitCode != 0 {
				t.Fatalf("exit code = %d, stderr = %q", exitCode, stderr.String())
			}
			if stderr.Len() != 0 {
				t.Fatalf("stderr = %q, want empty", stderr.String())
			}
			for _, want := range test.want {
				if !strings.Contains(stdout.String(), want) {
					t.Errorf("completion output does not contain %q", want)
				}
			}
			if strings.Contains(stdout.String(), "--insecure") {
				t.Error("completion output advertises removed --insecure option")
			}
		})
	}
}

func TestCompletionRejectsUnsupportedShell(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	exitCode := Run([]string{"completion", "fish"}, &stdout, &stderr, func(string) string { return "" })
	if exitCode != 1 {
		t.Fatalf("exit code = %d, want 1", exitCode)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), `unsupported shell "fish": use bash or zsh`) {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestCompletionHelp(t *testing.T) {
	t.Parallel()

	for _, help := range []string{"--help", "-help", "-h"} {
		var stdout, stderr bytes.Buffer
		exitCode := Run([]string{"completion", help}, &stdout, &stderr, func(string) string { return "" })
		if exitCode != 0 {
			t.Errorf("%s: exit code = %d, stderr = %q", help, exitCode, stderr.String())
		}
		if stdout.String() != "Usage: dracli completion <bash|zsh>\n" {
			t.Errorf("%s: stdout = %q", help, stdout.String())
		}
	}
}

func TestVerifyTLSMayPrecedeCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want []string
	}{
		{name: "simple command", args: []string{"--verify-tls", "status", "10.46.96.160"}, want: []string{"status", "--verify-tls", "10.46.96.160"}},
		{name: "settings subcommand", args: []string{"--verify-tls", "settings", "bios", "10.46.96.160"}, want: []string{"settings", "bios", "--verify-tls", "10.46.96.160"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeGlobalArgs(test.args)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, test.want) {
				t.Fatalf("normalizeGlobalArgs() = %#v, want %#v", got, test.want)
			}
		})
	}

}

func TestIPv4AddressWithoutCommandDefaultsToQuery(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	exitCode := Run([]string{"10.46.96.160"}, io.Discard, &stderr, func(string) string { return "" })
	if exitCode != 1 || !strings.Contains(stderr.String(), "BMC_MASTER must be set") {
		t.Fatalf("exit code = %d, stderr = %q", exitCode, stderr.String())
	}

	args, err := normalizeGlobalArgs([]string{"--verify-tls", "10.46.96.160"})
	if err != nil {
		t.Fatal(err)
	}
	if net.ParseIP(args[0]) == nil {
		t.Fatalf("normalized commandless arguments = %#v", args)
	}
}

func TestPromptForNextLogPage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  bool
	}{
		{input: "\n", want: true},
		{input: "anything\n", want: true},
		{input: "Q\n", want: false},
		{input: "", want: false},
	}
	for _, test := range tests {
		var prompt bytes.Buffer
		got := promptForNextLogPage(bufio.NewReader(strings.NewReader(test.input)), &prompt)
		if got != test.want {
			t.Errorf("input %q: got %v, want %v", test.input, got, test.want)
		}
		if !strings.Contains(prompt.String(), "next page") {
			t.Errorf("input %q: prompt = %q", test.input, prompt.String())
		}
	}
}

func TestAllLogsExampleUsesSelectedCommand(t *testing.T) {
	t.Parallel()

	got := allLogsExample("sel-logs", "10.46.96.160", true, "json")
	want := "dracli sel-logs --all --verify-tls --output json 10.46.96.160"
	if got != want {
		t.Fatalf("allLogsExample() = %q, want %q", got, want)
	}
}

func TestWriteInventoryReportsPartialFailureAndClockWarning(t *testing.T) {
	t.Parallel()

	inventory := redfish.Inventory{
		System: redfish.SystemSummary{Manufacturer: "Dell", Model: "PowerEdge"},
		Clock:  redfish.ClockSummary{DriftSeconds: -75, Significant: true},
		Errors: map[string]string{"raid_controllers": redfish.UnableToParseRedfishResponse},
	}
	var output bytes.Buffer
	if err := writeInventory(&output, inventory, "text"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "System: Dell PowerEdge") ||
		!strings.Contains(output.String(), "*** WARNING:") ||
		!strings.Contains(output.String(), "RAID controllers:\n  UNABLE TO PARSE REDFISH RESPONSE") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestWriteQueryIncludesStatusWithoutDetailedInventory(t *testing.T) {
	t.Parallel()

	inventory := redfish.Inventory{
		System:       redfish.SystemSummary{Manufacturer: "Dell", Model: "PowerEdge"},
		ServiceTag:   "ABC1234",
		SerialNumber: "CNFCP004410014",
		IDRAC:        redfish.FirmwareSummary{Model: "iDRAC9", Version: "7.20"},
		Status: redfish.SystemStatus{
			PowerState: "On",
			BootProgress: redfish.BootProgress{
				LastState: "OSRunning", LastStateTime: "2026-09-10T07:00:00Z",
			},
		},
		Clock: redfish.ClockSummary{DriftSeconds: 1},
	}
	var output bytes.Buffer
	if err := writeQuery(&output, inventory, "text"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Service Tag: ABC1234") ||
		!strings.Contains(output.String(), "iDRAC: iDRAC9 7.20") ||
		!strings.Contains(output.String(), "Power state: On") ||
		!strings.Contains(output.String(), "Boot progress: OSRunning at 2026-09-10T07:00:00Z") {
		t.Fatalf("output = %q", output.String())
	}
	if strings.Contains(output.String(), "CNFCP004410014") {
		t.Fatalf("text output included the hardware serial number: %q", output.String())
	}
	if strings.Contains(output.String(), "RAID controllers:") || strings.Contains(output.String(), "NICs:") {
		t.Fatalf("query included slow inventory sections: %q", output.String())
	}
	var jsonOutput bytes.Buffer
	if err := writeQuery(&jsonOutput, inventory, "json"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(jsonOutput.String(), `"hardware_version": "iDRAC9"`) ||
		!strings.Contains(jsonOutput.String(), `"firmware_version": "7.20"`) ||
		!strings.Contains(jsonOutput.String(), `"service_tag": "ABC1234"`) ||
		!strings.Contains(jsonOutput.String(), `"serial_number": "CNFCP004410014"`) {
		t.Fatalf("JSON output = %q", jsonOutput.String())
	}
}

func TestQuerySystemSummaryOrder(t *testing.T) {
	t.Parallel()

	inventory := redfish.Inventory{
		System:      redfish.SystemSummary{Manufacturer: "Dell Inc.", Model: "PowerEdge XE8640"},
		ServiceTag:  "ABC1234",
		Memory:      redfish.MemorySummary{TotalGiB: 2048},
		CPU:         redfish.ProcessorSummary{Count: 2, Model: "Intel Xeon", Cores: 96, Threads: 192},
		BIOSVersion: "2.8.2",
		IDRAC:       redfish.FirmwareSummary{Model: "16G Monolithic", Version: "7.30.10.50"},
	}
	var output bytes.Buffer
	if err := writeQuery(&output, inventory, "text"); err != nil {
		t.Fatal(err)
	}
	want := []string{"System:", "Service Tag:", "Memory:", "CPU:", "BIOS:", "iDRAC:"}
	previous := -1
	for _, label := range want {
		index := strings.Index(output.String(), label)
		if index <= previous {
			t.Fatalf("%s is out of order in %q", label, output.String())
		}
		previous = index
	}
}

func TestTLSVerificationOverridesInsecureDefault(t *testing.T) {
	t.Parallel()

	if !shouldSkipTLSVerification(false) {
		t.Fatal("default should skip TLS verification")
	}
	if shouldSkipTLSVerification(true) {
		t.Fatal("--verify-tls should enable verification")
	}
}

func TestWriteSettingsPreservesCuratedOrderAndReportsHostname(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	settings := redfish.SettingsSelection{
		Attributes:      map[string]any{"second": nil, "first": "value"},
		Order:           []string{"first", "second"},
		IncludeHostname: true,
	}
	if err := writeSettings(&output, settings, "text"); err != nil {
		t.Fatal(err)
	}
	want := "Hostname: unknown\nfirst: value\nsecond: not reported\n"
	if output.String() != want {
		t.Fatalf("output = %q, want %q", output.String(), want)
	}
}

func TestWriteJobs(t *testing.T) {
	t.Parallel()

	jobs := []redfish.Job{{
		ID: "JID_1", State: "Scheduled", Type: "Configuration",
		PercentComplete: 20, Message: "Task successfully scheduled.",
	}}
	var output bytes.Buffer
	if err := writeJobs(&output, jobs, "text"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "JID_1 state=Scheduled progress=20% type=Configuration") ||
		!strings.Contains(output.String(), "message=Task successfully scheduled.") {
		t.Fatalf("output = %q", output.String())
	}
	output.Reset()
	if err := writeJobs(&output, nil, "text"); err != nil {
		t.Fatal(err)
	}
	if output.String() != "No jobs in queue.\n" {
		t.Fatalf("empty output = %q", output.String())
	}
}

func TestParseSettingsAssignments(t *testing.T) {
	t.Parallel()

	values, err := parseSettingsAssignments([]string{
		"SecureBoot=Disabled",
		"SNMP.1.AlertPort=161",
		`Label="maintenance host"`,
		"Enabled=true",
	})
	if err != nil {
		t.Fatal(err)
	}
	if values["SecureBoot"] != "Disabled" || values["SNMP.1.AlertPort"] != float64(161) ||
		values["Label"] != "maintenance host" || values["Enabled"] != true {
		t.Fatalf("values = %#v", values)
	}
	if _, err := parseSettingsAssignments([]string{"SecureBoot"}); err == nil {
		t.Fatal("assignment without equals sign was accepted")
	}
}

func TestWriteSettingsGroups(t *testing.T) {
	t.Parallel()

	drac := redfish.SettingsSelection{Hostname: "idrac01", IncludeHostname: true, Attributes: map[string]any{"NTP": "Enabled"}}
	bios := redfish.SettingsSelection{Attributes: map[string]any{"SecureBoot": "Disabled"}}
	var output bytes.Buffer
	if err := writeSettingsGroups(&output, drac, bios, "text"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "iDRAC settings:\nHostname: idrac01") ||
		!strings.Contains(output.String(), "BIOS settings:\nSecureBoot: Disabled") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestSettingsRejectsAmbiguousOrConflictingModesBeforeConnecting(t *testing.T) {
	t.Parallel()

	tests := []struct {
		args []string
		want string
	}{
		{args: []string{"settings", "--set", "SecureBoot=Disabled", "10.46.96.160"}, want: "requires an explicit settings namespace"},
		{args: []string{"settings", "bios", "--all", "--name", "SecureBoot", "10.46.96.160"}, want: "--all and --name cannot be used together"},
	}
	for _, test := range tests {
		var stderr bytes.Buffer
		if exitCode := Run(test.args, io.Discard, &stderr, func(string) string { return "" }); exitCode != 1 {
			t.Fatalf("args %v: exit code = %d", test.args, exitCode)
		}
		if !strings.Contains(stderr.String(), test.want) {
			t.Fatalf("args %v: stderr = %q", test.args, stderr.String())
		}
	}
}

func TestFactoryResetRequiresExplicitConfirmationBeforeConnecting(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	exitCode := Run([]string{"factory-reset", "10.46.96.160"}, io.Discard, &stderr, func(string) string { return "" })
	if exitCode != 1 {
		t.Fatalf("exit code = %d, want 1", exitCode)
	}
	if !strings.Contains(stderr.String(), "without --yes") {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if strings.Contains(stderr.String(), "BMC_MASTER") {
		t.Fatalf("factory reset attempted credential resolution before confirmation: %q", stderr.String())
	}
}
