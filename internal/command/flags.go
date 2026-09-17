package command

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"time"
)

type commonFlags struct {
	username  string
	password  string
	output    string
	manager   string
	system    string
	timeout   time.Duration
	verifyTLS bool
}

var errHelpRequested = errors.New("help requested")

func newFlagSet(name string, stdout io.Writer, usageLine string) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(stdout)
	flags.Usage = func() {
		_, _ = fmt.Fprintln(stdout, usageLine)
		_, _ = fmt.Fprintln(stdout)
		flags.PrintDefaults()
	}
	return flags
}

func addCommonFlags(flags *flag.FlagSet, getenv func(string) string, opts *commonFlags, manager, system bool) {
	flags.StringVar(&opts.password, "password", "", "iDRAC password (otherwise IDRAC_PASSWORD or derived using BMC_MASTER)")
	flags.StringVar(&opts.username, "username", envOrDefault(getenv, "IDRAC_USERNAME", "root"), "iDRAC username")
	flags.BoolVar(&opts.verifyTLS, "verify-tls", false, "validate the iDRAC TLS certificate and hostname")
	flags.StringVar(&opts.output, "output", "text", "output format: text or json")
	flags.DurationVar(&opts.timeout, "timeout", 30*time.Second, "HTTP request timeout")
	if manager {
		flags.StringVar(&opts.manager, "manager", "iDRAC.Embedded.1", "Redfish manager identifier")
	}
	if system {
		flags.StringVar(&opts.system, "system", "System.Embedded.1", "Redfish system identifier")
	}
}

func parseBMCCommand(flags *flag.FlagSet, args []string) error {
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return errHelpRequested
		}
		return err
	}
	if flags.NArg() != 1 {
		flags.Usage()
		return errors.New("exactly one iDRAC IPv4 address is required")
	}
	return nil
}

func ignoreHelp(err error) error {
	if errors.Is(err, errHelpRequested) {
		return nil
	}
	return err
}

func requireOutputFormat(format string) error {
	if format != "text" && format != "json" {
		return fmt.Errorf("invalid output format %q: use text or json", format)
	}
	return nil
}
