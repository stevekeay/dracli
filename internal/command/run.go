package command

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/stevekeay/dracli/internal/credentials"
	"github.com/stevekeay/dracli/internal/redfish"
)

const usage = `dracli operates Dell iDRAC controllers through the Redfish API.

Usage:
  dracli [global options] <command> [command options] <iDRAC IPv4 address>
  dracli --help

Global options:
  --verify-tls  Validate the iDRAC TLS certificate and hostname. Verification is
                disabled by default. May also follow the command.
  --help        Show this guide. The aliases -h, -help, and help also work.

Commands:
  completion [bash|zsh]
      Generate a completion script for the selected shell. Source the output
      from your shell configuration to complete commands, options, and known
      option values.

  logs [system|lc]
      Fetch the System Event Log, Lifecycle Controller log, or both. With no
      selector, the complete system log is shown first, followed by the paged
      Lifecycle Controller log. Use --all to fetch every lifecycle page.

  password
      Resolve the iDRAC password and print it in plaintext to standard output.

  query
      Quickly show system, firmware, memory, CPU, power/boot status, and the
      iDRAC clock comparison. This is the default command when only an IP is
      supplied.

  inventory
      Show the query summary plus RAID controllers, attached disks, and NIC
      details. NIC output includes FQDD/slot, make/model, MAC, link, speed, and
      LLDP when available.

  status
      Show the current power state and boot progress with timestamps. Add
      --monitor to poll every five seconds and print only state changes; use
      --interval to select a different polling period.

  jobs
      Show the current iDRAC job queue, including state, progress, type,
      message, and timestamps.

  clear-jobs
      Delete every entry in the iDRAC job queue. This operation cannot be
      undone, does not restart Lifecycle Controller services, and requires
      --yes.

  factory-reset
      Reset iDRAC settings to factory defaults while preserving its network
      configuration and user accounts. This disruptive operation requires
      --yes.

  settings [idrac|bios]
      Show curated settings for both iDRAC and BIOS, or select one namespace.
      Add --all for every available attribute or repeat --name to select
      attributes. To change values, select idrac or bios and repeat
      --set NAME=VALUE. Unquoted values are parsed as JSON when possible.

Common command options (place these after the command):
  --username NAME    iDRAC username; defaults to IDRAC_USERNAME or root.
  --password VALUE   Plaintext iDRAC password override.
  --output FORMAT    Select text (default) or json.
  --timeout DURATION HTTP request timeout; defaults to 30s.
  --manager ID       Override the Redfish manager ID where applicable.
  --system ID        Override the Redfish system ID where applicable.

Credentials:
  The username defaults to root. Password precedence is --password, then
  IDRAC_PASSWORD, then derivation from BMC_MASTER. BMC_MASTER is required only
  when neither of the password overrides is supplied.

Output:
  Commands produce concise text by default. Use --output json after a command
  for machine-readable output. Monitored JSON output is JSON Lines.

Examples:
  source <(dracli completion bash)
  source <(dracli completion zsh)
  dracli status x.x.x.x
  dracli status --monitor x.x.x.x
  dracli query --output json x.x.x.x
  dracli logs x.x.x.x
  dracli logs system x.x.x.x
  dracli logs lc --all x.x.x.x
  dracli password x.x.x.x
  dracli clear-jobs --yes x.x.x.x
  dracli factory-reset --yes x.x.x.x
  dracli settings --name SecureBoot --name TimeZone x.x.x.x
  dracli settings bios --set SecureBoot=Disabled x.x.x.x
  dracli --verify-tls settings bios x.x.x.x

Run "dracli <command> --help" for command options.
`

func Run(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	return run(args, nil, stdout, stderr, getenv, false)
}

// RunCLI enables terminal-only behavior such as progress display and
// interactive log pagination.
func RunCLI(args []string, stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string) int {
	interactive := isTerminal(stdin) && isTerminal(stdout)
	description := progressDescription(args)
	if !isTerminal(stdout) || description == "" {
		return run(args, stdin, stdout, stderr, getenv, interactive)
	}

	progress := newTerminalProgress(stdout, description)
	defer progress.Stop()
	return run(args, stdin, progress.StopOnWrite(stdout), progress.StopOnWrite(stderr), getenv, interactive)
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string, interactive bool) int {
	if len(args) == 0 {
		_, _ = io.WriteString(stderr, usage)
		return 2
	}
	var err error
	args, err = normalizeGlobalArgs(args)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "dracli: %v\nRun \"dracli --help\" for usage.\n", err)
		return 2
	}
	if net.ParseIP(args[0]) != nil {
		args = append([]string{"query"}, args...)
	}

	switch args[0] {
	case "help", "-help", "--help", "-h":
		_, _ = io.WriteString(stdout, usage)
		return 0
	case "completion":
		if err := runCompletion(args[1:], stdout); err != nil {
			_, _ = fmt.Fprintf(stderr, "dracli: %v\n", err)
			return 1
		}
		return 0
	case "logs":
		if err := runLogs(args[1:], stdin, stdout, stderr, getenv, interactive); err != nil {
			_, _ = fmt.Fprintf(stderr, "dracli: %v\n", err)
			return 1
		}
		return 0
	case "password":
		if err := runPassword(args[1:], stdout, getenv); err != nil {
			_, _ = fmt.Fprintf(stderr, "dracli: %v\n", err)
			return 1
		}
		return 0
	case "query":
		if err := runInventory(args[1:], stdout, stderr, getenv, false); err != nil {
			_, _ = fmt.Fprintf(stderr, "dracli: %v\n", err)
			return 1
		}
		return 0
	case "inventory":
		if err := runInventory(args[1:], stdout, stderr, getenv, true); err != nil {
			_, _ = fmt.Fprintf(stderr, "dracli: %v\n", err)
			return 1
		}
		return 0
	case "status":
		if err := runStatus(args[1:], stdout, stderr, getenv); err != nil {
			_, _ = fmt.Fprintf(stderr, "dracli: %v\n", err)
			return 1
		}
		return 0
	case "jobs":
		if err := runJobs(args[1:], stdout, stderr, getenv, false); err != nil {
			_, _ = fmt.Fprintf(stderr, "dracli: %v\n", err)
			return 1
		}
		return 0
	case "clear-jobs":
		if err := runJobs(args[1:], stdout, stderr, getenv, true); err != nil {
			_, _ = fmt.Fprintf(stderr, "dracli: %v\n", err)
			return 1
		}
		return 0
	case "factory-reset":
		if err := runFactoryReset(args[1:], stdout, stderr, getenv); err != nil {
			_, _ = fmt.Fprintf(stderr, "dracli: %v\n", err)
			return 1
		}
		return 0
	case "settings":
		if err := runSettings(args[1:], stdout, stderr, getenv); err != nil {
			_, _ = fmt.Fprintf(stderr, "dracli: %v\n", err)
			return 1
		}
		return 0
	default:
		_, _ = fmt.Fprintf(stderr, "dracli: unknown command %q\nRun \"dracli --help\" for a list of commands.\n", args[0])
		return 2
	}
}

// normalizeGlobalArgs permits generic flags before the command while the
// command FlagSet remains the single place where their values are parsed.
func normalizeGlobalArgs(args []string) ([]string, error) {
	prefix := make([]string, 0, 1)
	for index, argument := range args {
		switch {
		case argument == "--help" || argument == "-help" || argument == "-h" || argument == "help":
			return []string{"help"}, nil
		case argument == "--verify-tls" || argument == "-verify-tls" ||
			strings.HasPrefix(argument, "--verify-tls=") || strings.HasPrefix(argument, "-verify-tls="):
			prefix = append(prefix, argument)
		default:
			if strings.HasPrefix(argument, "-") {
				return nil, fmt.Errorf("unknown global option %q", argument)
			}
			normalized := make([]string, 0, len(args))
			normalized = append(normalized, argument)
			if index+1 < len(args) && ((argument == "settings" && (args[index+1] == "idrac" || args[index+1] == "bios")) ||
				(argument == "logs" && (args[index+1] == "system" || args[index+1] == "lc"))) {
				normalized = append(normalized, args[index+1])
				normalized = append(normalized, prefix...)
				normalized = append(normalized, args[index+2:]...)
				return normalized, nil
			}
			normalized = append(normalized, prefix...)
			normalized = append(normalized, args[index+1:]...)
			return normalized, nil
		}
	}
	return nil, errors.New("a command is required")
}

func runLogs(args []string, stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string, interactive bool) error {
	selection := ""
	if len(args) > 0 && (args[0] == "system" || args[0] == "lc") {
		selection = args[0]
		args = args[1:]
	}
	flags := newFlagSet("logs", stdout, "Usage: dracli logs [system|lc] [options] <iDRAC IPv4 address>")
	var opts commonFlags
	addCommonFlags(flags, getenv, &opts, true, false)
	all := flags.Bool("all", false, "fetch every available log page without prompting")
	if err := parseBMCCommand(flags, args); err != nil {
		return ignoreHelp(err)
	}
	if err := requireOutputFormat(opts.output); err != nil {
		return err
	}

	client, err := newRedfishClient(flags.Arg(0), opts.username, opts.password, shouldSkipTLSVerification(opts.verifyTLS), opts.timeout, getenv, stdout)
	if err != nil {
		return err
	}

	if selection == "" {
		return runCombinedLogs(client, flags.Arg(0), opts, *all, stdin, stdout, stderr, interactive)
	}
	return runSelectedLogs(client, selection, flags.Arg(0), opts, *all, stdin, stdout, stderr, interactive)
}

type logClient interface {
	LifecycleLogs(context.Context, string) ([]json.RawMessage, error)
	SystemEventLogs(context.Context, string) ([]json.RawMessage, error)
	LifecycleLogPages(context.Context, string, func(redfish.LogPage) (bool, error)) error
	SystemEventLogPages(context.Context, string, func(redfish.LogPage) (bool, error)) error
}

func runCombinedLogs(client logClient, host string, opts commonFlags, all bool, stdin io.Reader, stdout, stderr io.Writer, interactive bool) error {
	systemEntries, err := client.SystemEventLogs(context.Background(), opts.manager)
	if err != nil {
		return err
	}
	if opts.output == "json" {
		lifecycleEntries, more, err := lifecycleLogEntries(client, opts.manager, all)
		if err != nil {
			return err
		}
		if more {
			printMoreLogsWarning(stderr, 1, allLogsExample("lc", host, opts.verifyTLS, opts.output))
		}
		return writeCombinedLogEntries(stdout, systemEntries, lifecycleEntries)
	}
	if _, err := fmt.Fprintln(stdout, "System Event Log:"); err != nil {
		return fmt.Errorf("write text output: %w", err)
	}
	if err := writeEntries(stdout, systemEntries, opts.output); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(stdout, "\nLifecycle Controller Log:"); err != nil {
		return fmt.Errorf("write text output: %w", err)
	}
	return pageSelectedLogs(client, "lc", host, opts, all, stdin, stdout, stderr, interactive)
}

func runSelectedLogs(client logClient, selection, host string, opts commonFlags, all bool, stdin io.Reader, stdout, stderr io.Writer, interactive bool) error {
	if all {
		var entries []json.RawMessage
		var err error
		if selection == "system" {
			entries, err = client.SystemEventLogs(context.Background(), opts.manager)
		} else {
			entries, err = client.LifecycleLogs(context.Background(), opts.manager)
		}
		if err != nil {
			return err
		}
		return writeEntries(stdout, entries, opts.output)
	}
	return pageSelectedLogs(client, selection, host, opts, false, stdin, stdout, stderr, interactive)
}

func pageSelectedLogs(client logClient, selection, host string, opts commonFlags, all bool, stdin io.Reader, stdout, stderr io.Writer, interactive bool) error {
	systemEvent := selection == "system"
	if all {
		var entries []json.RawMessage
		var err error
		if systemEvent {
			entries, err = client.SystemEventLogs(context.Background(), opts.manager)
		} else {
			entries, err = client.LifecycleLogs(context.Background(), opts.manager)
		}
		if err != nil {
			return err
		}
		return writeEntries(stdout, entries, opts.output)
	}

	reader := bufio.NewReader(stdin)
	visit := func(page redfish.LogPage) (bool, error) {
		if err := writeEntries(stdout, page.Entries, opts.output); err != nil {
			return false, err
		}
		if !page.More {
			return false, nil
		}
		if !interactive || opts.output == "json" {
			printMoreLogsWarning(stderr, page.Number, allLogsExample(selection, host, opts.verifyTLS, opts.output))
			return false, nil
		}
		return promptForNextLogPage(reader, stderr), nil
	}
	if systemEvent {
		return client.SystemEventLogPages(context.Background(), opts.manager, visit)
	}
	return client.LifecycleLogPages(context.Background(), opts.manager, visit)
}

func lifecycleLogEntries(client logClient, manager string, all bool) ([]json.RawMessage, bool, error) {
	if all {
		entries, err := client.LifecycleLogs(context.Background(), manager)
		return entries, false, err
	}
	var entries []json.RawMessage
	more := false
	err := client.LifecycleLogPages(context.Background(), manager, func(page redfish.LogPage) (bool, error) {
		entries = append(entries, page.Entries...)
		more = page.More
		return false, nil
	})
	return entries, more, err
}

func printMoreLogsWarning(output io.Writer, page int, example string) {
	_, _ = fmt.Fprintf(output, "dracli: page %d fetched; more log entries are available; add --all (for example: %s)\n", page, example)
}

func promptForNextLogPage(reader *bufio.Reader, output io.Writer) bool {
	_, _ = fmt.Fprint(output, "More log entries are available; press Enter for the next page, or q then Enter to quit: ")
	answer, err := reader.ReadString('\n')
	if err != nil && len(answer) == 0 {
		_, _ = fmt.Fprintln(output)
		return false
	}
	return !strings.EqualFold(strings.TrimSpace(answer), "q")
}

func allLogsExample(selection, host string, verifyTLS bool, output string) string {
	parts := []string{"dracli", "logs"}
	if selection != "" {
		parts = append(parts, selection)
	}
	parts = append(parts, "--all")
	if verifyTLS {
		parts = append(parts, "--verify-tls")
	}
	if output != "text" {
		parts = append(parts, "--output", output)
	}
	return strings.Join(append(parts, host), " ")
}

func runPassword(args []string, stdout io.Writer, getenv func(string) string) error {
	flags := newFlagSet("password", stdout, "Usage: dracli password [options] <iDRAC IPv4 address>")
	explicit := flags.String("password", "", "plaintext iDRAC password override")
	if err := parseBMCCommand(flags, args); err != nil {
		return ignoreHelp(err)
	}
	host := flags.Arg(0)
	if ip := net.ParseIP(host); ip == nil || ip.To4() == nil {
		return fmt.Errorf("need an IPv4 address, not %q", host)
	}
	password, err := credentials.Resolve(host, *explicit, getenv)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(stdout, password); err != nil {
		return fmt.Errorf("write password: %w", err)
	}
	return nil
}

func isTerminal(value any) bool {
	file, ok := value.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func runInventory(args []string, stdout, stderr io.Writer, getenv func(string) string, detailed bool) error {
	commandName := "query"
	if detailed {
		commandName = "inventory"
	}
	flags := flag.NewFlagSet(commandName, flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "Usage: dracli %s [options] <BMC IPv4 address>\n", commandName)
		_, _ = fmt.Fprintln(stderr)
		flags.PrintDefaults()
	}
	password := flags.String("password", "", "BMC password (otherwise DRAC_PASSWORD or derived using BMC_MASTER)")
	username := flags.String("username", envOrDefault(getenv, "DRAC_USERNAME", "root"), "BMC username")
	verifyTLS := flags.Bool("verify-tls", false, "validate the BMC TLS certificate and hostname")
	output := flags.String("output", "text", "output format: text or json")
	manager := flags.String("manager", "iDRAC.Embedded.1", "Redfish manager identifier")
	system := flags.String("system", "System.Embedded.1", "Redfish system identifier")
	timeout := flags.Duration("timeout", 30*time.Second, "HTTP request timeout")

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 1 {
		flags.Usage()
		return errors.New("exactly one BMC IPv4 address is required")
	}
	if *output != "text" && *output != "json" {
		return fmt.Errorf("invalid output format %q: use text or json", *output)
	}

	client, err := newRedfishClient(flags.Arg(0), *username, *password, shouldSkipTLSVerification(*verifyTLS), *timeout, getenv, stdout)
	if err != nil {
		return err
	}
	var inventory redfish.Inventory
	if detailed {
		inventory, err = client.Inventory(context.Background(), *system, *manager)
	} else {
		inventory, err = client.Query(context.Background(), *system, *manager)
	}
	if err != nil {
		return err
	}
	if !detailed {
		return writeQuery(stdout, inventory, *output)
	}
	return writeInventory(stdout, inventory, *output)
}

func runStatus(args []string, stdout, stderr io.Writer, getenv func(string) string) error {
	flags := flag.NewFlagSet("status", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "Usage: dracli status [options] <BMC IPv4 address>")
		_, _ = fmt.Fprintln(stderr)
		flags.PrintDefaults()
	}
	password := flags.String("password", "", "BMC password (otherwise DRAC_PASSWORD or derived using BMC_MASTER)")
	username := flags.String("username", envOrDefault(getenv, "DRAC_USERNAME", "root"), "BMC username")
	verifyTLS := flags.Bool("verify-tls", false, "validate the BMC TLS certificate and hostname")
	output := flags.String("output", "text", "output format: text or json")
	system := flags.String("system", "System.Embedded.1", "Redfish system identifier")
	timeout := flags.Duration("timeout", 30*time.Second, "HTTP request timeout")
	monitor := flags.Bool("monitor", false, "poll and report power or boot-progress changes")
	interval := flags.Duration("interval", 5*time.Second, "monitor polling interval")

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 1 {
		flags.Usage()
		return errors.New("exactly one BMC IPv4 address is required")
	}
	if *output != "text" && *output != "json" {
		return fmt.Errorf("invalid output format %q: use text or json", *output)
	}
	if *interval <= 0 {
		return errors.New("interval must be greater than zero")
	}

	client, err := newRedfishClient(flags.Arg(0), *username, *password, shouldSkipTLSVerification(*verifyTLS), *timeout, getenv, stdout)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	current, err := client.SystemStatus(ctx, *system)
	if err != nil {
		return err
	}
	if err := writeStatus(stdout, current, time.Now(), *output, *monitor, false); err != nil {
		return err
	}
	if !*monitor {
		return nil
	}

	ticker := time.NewTicker(*interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			next, err := client.SystemStatus(ctx, *system)
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
			if next != current {
				if err := writeStatus(stdout, next, time.Now(), *output, true, true); err != nil {
					return err
				}
				current = next
			}
		}
	}
}

func runJobs(args []string, stdout, stderr io.Writer, getenv func(string) string, clear bool) error {
	commandName := "jobs"
	if clear {
		commandName = "clear-jobs"
	}
	flags := flag.NewFlagSet(commandName, flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "Usage: dracli %s [options] <BMC IPv4 address>\n\n", commandName)
		flags.PrintDefaults()
	}
	password := flags.String("password", "", "BMC password (otherwise DRAC_PASSWORD or derived using BMC_MASTER)")
	username := flags.String("username", envOrDefault(getenv, "DRAC_USERNAME", "root"), "BMC username")
	verifyTLS := flags.Bool("verify-tls", false, "validate the BMC TLS certificate and hostname")
	output := flags.String("output", "text", "output format: text or json")
	manager := flags.String("manager", "iDRAC.Embedded.1", "Redfish manager identifier")
	timeout := flags.Duration("timeout", 30*time.Second, "HTTP request timeout")

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 1 {
		flags.Usage()
		return errors.New("exactly one BMC IPv4 address is required")
	}
	if *output != "text" && *output != "json" {
		return fmt.Errorf("invalid output format %q: use text or json", *output)
	}
	client, err := newRedfishClient(flags.Arg(0), *username, *password, shouldSkipTLSVerification(*verifyTLS), *timeout, getenv, stdout)
	if err != nil {
		return err
	}
	if clear {
		if err := client.ClearJobs(context.Background(), *manager); err != nil {
			return err
		}
		if *output == "json" {
			return json.NewEncoder(stdout).Encode(map[string]bool{"cleared": true})
		}
		_, err := fmt.Fprintln(stdout, "Job queue cleared.")
		return err
	}
	jobs, err := client.Jobs(context.Background(), *manager)
	if err != nil {
		return err
	}
	return writeJobs(stdout, jobs, *output)
}

func runFactoryReset(args []string, stdout, stderr io.Writer, getenv func(string) string) error {
	flags := flag.NewFlagSet("factory-reset", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "Usage: dracli factory-reset --yes [options] <BMC IPv4 address>")
		_, _ = fmt.Fprintln(stderr)
		flags.PrintDefaults()
	}
	password := flags.String("password", "", "BMC password (otherwise DRAC_PASSWORD or derived using BMC_MASTER)")
	username := flags.String("username", envOrDefault(getenv, "DRAC_USERNAME", "root"), "BMC username")
	verifyTLS := flags.Bool("verify-tls", false, "validate the BMC TLS certificate and hostname")
	output := flags.String("output", "text", "output format: text or json")
	manager := flags.String("manager", "iDRAC.Embedded.1", "Redfish manager identifier")
	timeout := flags.Duration("timeout", 30*time.Second, "HTTP request timeout")
	confirmed := flags.Bool("yes", false, "confirm the factory reset without prompting")

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 1 {
		flags.Usage()
		return errors.New("exactly one BMC IPv4 address is required")
	}
	if *output != "text" && *output != "json" {
		return fmt.Errorf("invalid output format %q: use text or json", *output)
	}
	if !*confirmed {
		return errors.New("refusing to reset iDRAC settings without --yes")
	}

	client, err := newRedfishClient(flags.Arg(0), *username, *password, shouldSkipTLSVerification(*verifyTLS), *timeout, getenv, stdout)
	if err != nil {
		return err
	}
	if err := client.ResetToDefaults(context.Background(), *manager); err != nil {
		return err
	}
	if *output == "json" {
		return json.NewEncoder(stdout).Encode(map[string]any{
			"accepted":            true,
			"reset_type":          "Default",
			"preserves_network":   true,
			"preserves_user_data": true,
		})
	}
	_, err = fmt.Fprintln(stdout, "iDRAC factory reset accepted; network configuration and user accounts are preserved.")
	return err
}

func runSettings(args []string, stdout, stderr io.Writer, getenv func(string) string) error {
	kind := ""
	if len(args) > 0 && (args[0] == "drac" || args[0] == "bios") {
		kind = args[0]
		args = args[1:]
	}
	commandName := "settings"
	if kind != "" {
		commandName += " " + kind
	}
	flags := flag.NewFlagSet(commandName, flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "Usage: dracli settings [drac|bios] [options] <BMC IPv4 address>")
		flags.PrintDefaults()
	}
	password := flags.String("password", "", "BMC password (otherwise DRAC_PASSWORD or derived using BMC_MASTER)")
	username := flags.String("username", envOrDefault(getenv, "DRAC_USERNAME", "root"), "BMC username")
	verifyTLS := flags.Bool("verify-tls", false, "validate the BMC TLS certificate and hostname")
	output := flags.String("output", "text", "output format: text or json")
	manager := flags.String("manager", "iDRAC.Embedded.1", "Redfish manager identifier")
	system := flags.String("system", "System.Embedded.1", "Redfish system identifier")
	timeout := flags.Duration("timeout", 30*time.Second, "HTTP request timeout")
	all := flags.Bool("all", false, "show every available setting")
	var names stringListFlag
	var assignments stringListFlag
	flags.Var(&names, "name", "show this setting (repeatable)")
	flags.Var(&assignments, "set", "set NAME=VALUE; select drac or bios explicitly (repeatable)")

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 1 {
		flags.Usage()
		return errors.New("exactly one BMC IPv4 address is required")
	}
	if *output != "text" && *output != "json" {
		return fmt.Errorf("invalid output format %q: use text or json", *output)
	}
	if *all && len(names) > 0 {
		return errors.New("--all and --name cannot be used together")
	}
	if len(assignments) > 0 && (*all || len(names) > 0) {
		return errors.New("--set cannot be combined with --all or --name")
	}
	if len(assignments) > 0 && kind == "" {
		return errors.New("--set requires an explicit settings namespace: drac or bios")
	}

	client, err := newRedfishClient(flags.Arg(0), *username, *password, shouldSkipTLSVerification(*verifyTLS), *timeout, getenv, stdout)
	if err != nil {
		return err
	}
	ctx := context.Background()
	if len(assignments) > 0 {
		values, parseErr := parseSettingsAssignments(assignments)
		if parseErr != nil {
			return parseErr
		}
		if kind == "drac" {
			err = client.SetDRACSettings(ctx, *manager, values)
		} else {
			err = client.SetBIOSSettings(ctx, *system, values)
		}
		if err != nil {
			return err
		}
		return writeSettingsUpdate(stdout, kind, values, *output)
	}

	load := func(namespace string) (redfish.SettingsSelection, error) {
		switch {
		case namespace == "drac" && *all:
			return client.AllDRACSettings(ctx, *manager)
		case namespace == "drac" && len(names) > 0:
			return client.SelectedDRACSettings(ctx, *manager, names)
		case namespace == "drac":
			return client.DRACSettings(ctx, *manager)
		case namespace == "bios" && *all:
			return client.AllBIOSSettings(ctx, *system)
		case namespace == "bios" && len(names) > 0:
			return client.SelectedBIOSSettings(ctx, *system, names)
		default:
			return client.BIOSSettings(ctx, *system)
		}
	}
	if kind != "" {
		settings, loadErr := load(kind)
		if loadErr != nil {
			return loadErr
		}
		return writeSettings(stdout, settings, *output)
	}
	dracSettings, err := load("drac")
	if err != nil {
		return fmt.Errorf("query iDRAC settings: %w", err)
	}
	biosSettings, err := load("bios")
	if err != nil {
		return fmt.Errorf("query BIOS settings: %w", err)
	}
	return writeSettingsGroups(stdout, dracSettings, biosSettings, *output)
}

type stringListFlag []string

func (values *stringListFlag) String() string {
	return strings.Join(*values, ",")
}

func (values *stringListFlag) Set(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("value must not be empty")
	}
	*values = append(*values, value)
	return nil
}

func parseSettingsAssignments(assignments []string) (map[string]any, error) {
	values := make(map[string]any, len(assignments))
	for _, assignment := range assignments {
		name, raw, found := strings.Cut(assignment, "=")
		name = strings.TrimSpace(name)
		if !found || name == "" {
			return nil, fmt.Errorf("invalid setting %q: expected NAME=VALUE", assignment)
		}
		var value any
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			value = raw
		}
		values[name] = value
	}
	return values, nil
}

func newRedfishClient(host, username, password string, skipTLSVerification bool, timeout time.Duration, getenv func(string) string, progressOutput io.Writer) (*redfish.Client, error) {
	ip := net.ParseIP(host)
	if ip == nil || ip.To4() == nil {
		return nil, fmt.Errorf("need an IPv4 address, not %q", host)
	}
	resolvedPassword, err := credentials.Resolve(host, password, getenv)
	if err != nil {
		return nil, err
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: skipTLSVerification}
	httpClient := &http.Client{Transport: transport, Timeout: timeout}
	client, err := redfish.NewClient("https://"+host, username, resolvedPassword, httpClient)
	if err != nil {
		return nil, err
	}
	if progress, ok := progressOutput.(interface{ Retrying(time.Duration) }); ok {
		client.SetRetryNotifier(progress.Retrying)
	}
	return client, nil
}

func shouldSkipTLSVerification(verifyTLS bool) bool {
	return !verifyTLS
}

func writeInventory(output io.Writer, inventory redfish.Inventory, format string) error {
	return writeInventoryDetails(output, inventory, format, true)
}

func writeQuery(output io.Writer, inventory redfish.Inventory, format string) error {
	return writeInventoryDetails(output, inventory, format, false)
}

func writeInventoryDetails(output io.Writer, inventory redfish.Inventory, format string, detailed bool) error {
	if format == "json" {
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(inventory); err != nil {
			return fmt.Errorf("write JSON output: %w", err)
		}
		return nil
	}

	lines := []string{
		inventoryLine(inventory, "system", "System", joinKnown(inventory.System.Manufacturer, inventory.System.Model)),
		inventoryLine(inventory, "service_tag", "Service Tag", known(inventory.ServiceTag)),
		inventoryLine(inventory, "memory", "Memory", fmt.Sprintf("%g GiB", inventory.Memory.TotalGiB)),
		inventoryLine(inventory, "cpu", "CPU", fmt.Sprintf("%d x %s (%d cores, %d threads)", inventory.CPU.Count, known(inventory.CPU.Model), inventory.CPU.Cores, inventory.CPU.Threads)),
		inventoryLine(inventory, "bios", "BIOS", known(inventory.BIOSVersion)),
		inventoryLine(inventory, "idrac", "iDRAC", joinKnown(inventory.IDRAC.Model, inventory.IDRAC.Version)),
	}
	if message, failed := inventory.Errors["status"]; failed {
		lines = append(lines, "Status: "+message)
	} else {
		lines = append(lines, "Power state: "+known(inventory.Status.PowerState))
		boot := known(inventory.Status.BootProgress.LastState)
		if inventory.Status.BootProgress.LastStateTime != "" {
			boot += " at " + inventory.Status.BootProgress.LastStateTime
		}
		lines = append(lines, "Boot progress: "+boot)
	}
	if message, failed := inventory.Errors["clock"]; failed {
		lines = append(lines, "DRAC clock: "+message)
	} else {
		drift := inventory.Clock.DriftSeconds
		if drift < 0 {
			drift = -drift
		}
		if inventory.Clock.Significant {
			lines = append(lines, fmt.Sprintf("*** WARNING: DRAC CLOCK DIFFERS FROM LOCAL SYSTEM TIME BY %d SECONDS ***", drift))
		} else {
			lines = append(lines, fmt.Sprintf("DRAC clock agrees with local system time within %d seconds", drift))
		}
	}
	if !detailed {
		if _, err := fmt.Fprintln(output, strings.Join(lines, "\n")); err != nil {
			return fmt.Errorf("write text output: %w", err)
		}
		return nil
	}
	lines = append(lines, "RAID controllers:")
	if message, failed := inventory.Errors["raid_controllers"]; failed {
		lines = append(lines, "  "+message)
	} else if len(inventory.RAIDControllers) == 0 {
		lines = append(lines, "  none reported")
	}
	for _, controller := range inventory.RAIDControllers {
		details := joinKnown(controller.Manufacturer, controller.Model)
		if controller.Firmware != "" {
			details += " firmware " + controller.Firmware
		}
		lines = append(lines, fmt.Sprintf("  %s: %s", known(controller.ID), known(details)))
		if len(controller.Drives) == 0 {
			lines = append(lines, "    disks: none reported")
			continue
		}
		lines = append(lines, "    disks:")
		for _, drive := range controller.Drives {
			parts := []string{joinKnown(drive.Manufacturer, drive.Model)}
			if drive.CapacityBytes > 0 {
				parts = append(parts, formatCapacity(drive.CapacityBytes))
			}
			if drive.MediaType != "" {
				parts = append(parts, drive.MediaType)
			}
			if drive.Protocol != "" {
				parts = append(parts, drive.Protocol)
			}
			if drive.RotationSpeedRPM > 0 {
				parts = append(parts, fmt.Sprintf("%d RPM", drive.RotationSpeedRPM))
			}
			if drive.SerialNumber != "" {
				parts = append(parts, "serial "+drive.SerialNumber)
			}
			if drive.Firmware != "" {
				parts = append(parts, "firmware "+drive.Firmware)
			}
			if status := joinKnown(drive.Health, drive.State); status != "unknown" {
				parts = append(parts, "status "+status)
			}
			label := drive.ID
			if label == "" {
				label = drive.Name
			}
			lines = append(lines, fmt.Sprintf("      %s: %s", known(label), strings.Join(nonempty(parts), "; ")))
		}
	}
	lines = append(lines, "NICs:")
	if message, failed := inventory.Errors["nics"]; failed {
		lines = append(lines, "  "+message)
	} else if len(inventory.NICs) == 0 {
		lines = append(lines, "  none reported")
	}
	for _, nic := range inventory.NICs {
		parts := []string{joinKnown(nic.Manufacturer, nic.Model)}
		if len(nic.MACAddresses) > 0 {
			parts = append(parts, "MAC "+strings.Join(nic.MACAddresses, ","))
		}
		if nic.Link != "" {
			parts = append(parts, "link "+nic.Link)
		}
		if nic.SpeedMbps != nil {
			parts = append(parts, "speed "+formatSpeed(*nic.SpeedMbps))
		}
		if nic.LLDP != nil {
			parts = append(parts, "LLDP "+joinKnown(nic.LLDP.SwitchID, nic.LLDP.SwitchPort))
		}
		lines = append(lines, fmt.Sprintf("  %s: %s", nic.ID, strings.Join(nonempty(parts), "; ")))
	}
	if _, err := fmt.Fprintln(output, strings.Join(lines, "\n")); err != nil {
		return fmt.Errorf("write text output: %w", err)
	}
	return nil
}

func inventoryLine(inventory redfish.Inventory, section, label, value string) string {
	if message, failed := inventory.Errors[section]; failed {
		return label + ": " + message
	}
	return label + ": " + value
}

type observedStatus struct {
	ObservedAt   string               `json:"observed_at"`
	PowerState   string               `json:"power_state,omitempty"`
	BootProgress redfish.BootProgress `json:"boot_progress"`
}

func writeStatus(output io.Writer, status redfish.SystemStatus, observedAt time.Time, format string, monitoring, hasHistory bool) error {
	observation := observedStatus{
		ObservedAt:   observedAt.UTC().Format(time.RFC3339),
		PowerState:   status.PowerState,
		BootProgress: status.BootProgress,
	}
	if format == "json" {
		encoder := json.NewEncoder(output)
		if !monitoring {
			encoder.SetIndent("", "  ")
		}
		if err := encoder.Encode(observation); err != nil {
			return fmt.Errorf("write JSON output: %w", err)
		}
		return nil
	}

	since := ""
	if !hasHistory && status.BootProgress.LastStateTime != "" {
		since = " since=" + status.BootProgress.LastStateTime
	}
	if _, err := fmt.Fprintf(output, "%s power=%s boot=%s%s\n", observation.ObservedAt, known(status.PowerState), known(status.BootProgress.LastState), since); err != nil {
		return fmt.Errorf("write text output: %w", err)
	}
	return nil
}

func writeSettings(output io.Writer, settings redfish.SettingsSelection, format string) error {
	if format == "json" {
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(settings); err != nil {
			return fmt.Errorf("write JSON output: %w", err)
		}
		return nil
	}
	if settings.IncludeHostname {
		if _, err := fmt.Fprintf(output, "Hostname: %s\n", known(settings.Hostname)); err != nil {
			return fmt.Errorf("write text output: %w", err)
		}
	}
	keys := settings.Order
	if len(keys) == 0 {
		keys = make([]string, 0, len(settings.Attributes))
		for key := range settings.Attributes {
			keys = append(keys, key)
		}
		sort.Strings(keys)
	}
	for _, key := range keys {
		value := settings.Attributes[key]
		if value == nil {
			value = "not reported"
		}
		if _, err := fmt.Fprintf(output, "%s: %v\n", key, value); err != nil {
			return fmt.Errorf("write text output: %w", err)
		}
	}
	return nil
}

func writeJobs(output io.Writer, jobs []redfish.Job, format string) error {
	if format == "json" {
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(jobs); err != nil {
			return fmt.Errorf("write JSON output: %w", err)
		}
		return nil
	}
	if len(jobs) == 0 {
		_, err := fmt.Fprintln(output, "No jobs in queue.")
		return err
	}
	for _, job := range jobs {
		parts := []string{"state=" + known(job.State), fmt.Sprintf("progress=%d%%", job.PercentComplete)}
		if job.Type != "" {
			parts = append(parts, "type="+job.Type)
		}
		if job.Name != "" {
			parts = append(parts, "name="+job.Name)
		}
		if job.StartTime != "" {
			parts = append(parts, "start="+job.StartTime)
		}
		if job.CompletionTime != "" {
			parts = append(parts, "completed="+job.CompletionTime)
		}
		if job.Message != "" {
			parts = append(parts, "message="+job.Message)
		}
		if _, err := fmt.Fprintf(output, "%s %s\n", known(job.ID), strings.Join(parts, " ")); err != nil {
			return fmt.Errorf("write text output: %w", err)
		}
	}
	return nil
}

func writeSettingsGroups(output io.Writer, dracSettings, biosSettings redfish.SettingsSelection, format string) error {
	if format == "json" {
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(map[string]redfish.SettingsSelection{"drac": dracSettings, "bios": biosSettings}); err != nil {
			return fmt.Errorf("write JSON output: %w", err)
		}
		return nil
	}
	if _, err := fmt.Fprintln(output, "iDRAC settings:"); err != nil {
		return fmt.Errorf("write text output: %w", err)
	}
	if err := writeSettings(output, dracSettings, format); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(output, "\nBIOS settings:"); err != nil {
		return fmt.Errorf("write text output: %w", err)
	}
	return writeSettings(output, biosSettings, format)
}

func writeSettingsUpdate(output io.Writer, kind string, attributes map[string]any, format string) error {
	if format == "json" {
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(map[string]any{"namespace": kind, "accepted": true, "attributes": attributes}); err != nil {
			return fmt.Errorf("write JSON output: %w", err)
		}
		return nil
	}
	if _, err := fmt.Fprintf(output, "%s settings update accepted:\n", kind); err != nil {
		return fmt.Errorf("write text output: %w", err)
	}
	keys := make([]string, 0, len(attributes))
	for key := range attributes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, err := fmt.Fprintf(output, "%s: %v\n", key, attributes[key]); err != nil {
			return fmt.Errorf("write text output: %w", err)
		}
	}
	return nil
}

func known(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}

func joinKnown(values ...string) string {
	joined := strings.Join(nonempty(values), " ")
	return known(joined)
}

func nonempty(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && value != "unknown" {
			result = append(result, value)
		}
	}
	return result
}

func formatSpeed(mbps int) string {
	if mbps >= 1_000 && mbps%1_000 == 0 {
		return fmt.Sprintf("%d Gbps", mbps/1_000)
	}
	return fmt.Sprintf("%d Mbps", mbps)
}

func formatCapacity(bytes int64) string {
	const (
		gib = int64(1 << 30)
		tib = int64(1 << 40)
	)
	if bytes >= tib {
		return fmt.Sprintf("%.2f TiB", float64(bytes)/float64(tib))
	}
	return fmt.Sprintf("%.1f GiB", float64(bytes)/float64(gib))
}

func writeEntries(output io.Writer, entries []json.RawMessage, format string) error {
	if format == "json" {
		if entries == nil {
			entries = []json.RawMessage{}
		}
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(entries); err != nil {
			return fmt.Errorf("write JSON output: %w", err)
		}
		return nil
	}

	for _, raw := range entries {
		var entry redfish.LogEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			return fmt.Errorf("decode log entry: %w", err)
		}
		if _, err := fmt.Fprintf(output, "%s %s\n", entry.Created, entry.Message); err != nil {
			return fmt.Errorf("write text output: %w", err)
		}
	}
	return nil
}

func writeCombinedLogEntries(output io.Writer, system, lifecycle []json.RawMessage) error {
	if system == nil {
		system = []json.RawMessage{}
	}
	if lifecycle == nil {
		lifecycle = []json.RawMessage{}
	}
	result := struct {
		System []json.RawMessage `json:"system"`
		LC     []json.RawMessage `json:"lc"`
	}{System: system, LC: lifecycle}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		return fmt.Errorf("write JSON output: %w", err)
	}
	return nil
}

func envOrDefault(getenv func(string) string, key, fallback string) string {
	if value := getenv(key); value != "" {
		return value
	}
	return fallback
}
