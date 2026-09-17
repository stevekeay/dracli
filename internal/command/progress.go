package command

import (
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"
)

const spinnerInterval = 100 * time.Millisecond

var spinnerFrames = [...]string{"|", "/", "-", "\\"}

type terminalSpinner struct {
	output  io.Writer
	mu      sync.Mutex
	message string
	active  bool
	done    chan struct{}
	stopped chan struct{}
}

func startTerminalSpinner(output io.Writer, message string) *terminalSpinner {
	spinner := &terminalSpinner{
		output:  output,
		message: message,
		active:  true,
		done:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
	_, _ = fmt.Fprintf(output, "\r%s %s", spinnerFrames[0], message)
	go spinner.animate()
	return spinner
}

func (spinner *terminalSpinner) animate() {
	defer close(spinner.stopped)
	ticker := time.NewTicker(spinnerInterval)
	defer ticker.Stop()
	frame := 1
	for {
		select {
		case <-ticker.C:
			spinner.draw(spinnerFrames[frame])
			frame = (frame + 1) % len(spinnerFrames)
		case <-spinner.done:
			return
		}
	}
}

func (spinner *terminalSpinner) draw(frame string) {
	spinner.mu.Lock()
	defer spinner.mu.Unlock()
	if spinner.active {
		_, _ = fmt.Fprintf(spinner.output, "\r%s %s", frame, spinner.message)
	}
}

func (spinner *terminalSpinner) SetMessage(message string) {
	spinner.mu.Lock()
	defer spinner.mu.Unlock()
	if spinner.active {
		spinner.message = message
		_, _ = fmt.Fprintf(spinner.output, "\r\x1b[2K%s %s", spinnerFrames[0], message)
	}
}

func (spinner *terminalSpinner) Stop() {
	spinner.mu.Lock()
	spinner.active = false
	spinner.mu.Unlock()
	close(spinner.done)
	<-spinner.stopped
	_, _ = io.WriteString(spinner.output, "\r\x1b[2K")
}

type terminalProgress struct {
	spinner *terminalSpinner
	once    sync.Once
}

func newTerminalProgress(output io.Writer, message string) *terminalProgress {
	return &terminalProgress{spinner: startTerminalSpinner(output, message)}
}

func (progress *terminalProgress) Stop() {
	progress.once.Do(progress.spinner.Stop)
}

func (progress *terminalProgress) Retrying(delay time.Duration) {
	progress.spinner.SetMessage(fmt.Sprintf("iDRAC unavailable; retrying in %s...", delay))
}

func (progress *terminalProgress) StopOnWrite(output io.Writer) io.Writer {
	return progressWriter{progress: progress, output: output}
}

type progressWriter struct {
	progress *terminalProgress
	output   io.Writer
}

func (writer progressWriter) Write(value []byte) (int, error) {
	writer.progress.Stop()
	return writer.output.Write(value)
}

func (writer progressWriter) Retrying(delay time.Duration) {
	writer.progress.Retrying(delay)
}

func progressDescription(args []string) string {
	for _, argument := range args {
		if argument == "help" || argument == "--help" || argument == "-help" || argument == "-h" {
			return ""
		}
	}
	normalized, err := normalizeGlobalArgs(args)
	if err != nil || len(normalized) == 0 {
		return ""
	}
	command := normalized[0]
	if net.ParseIP(command) != nil {
		command = "query"
	}
	switch command {
	case "logs":
		return "Fetching Lifecycle Controller logs..."
	case "sel-logs":
		return "Fetching System Event Log..."
	case "query":
		return "Querying system summary..."
	case "inventory":
		return "Collecting hardware inventory..."
	case "status":
		return "Querying system status..."
	case "jobs":
		return "Fetching iDRAC job queue..."
	case "clear-jobs":
		return "Clearing iDRAC job queue..."
	case "factory-reset":
		return "Resetting iDRAC settings..."
	case "settings":
		for _, argument := range normalized[1:] {
			if argument == "--set" || argument == "-set" || strings.HasPrefix(argument, "--set=") || strings.HasPrefix(argument, "-set=") {
				return "Updating iDRAC settings..."
			}
		}
		return "Fetching iDRAC settings..."
	default:
		return ""
	}
}
