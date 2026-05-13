package tmux

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	readyPollInterval = 100 * time.Millisecond
	// sendKeysSettleDelay covers the gap between the final typed character and
	// the trailing Enter. Without it, tmux can deliver them in the same frame
	// and Claude Code drops the submit.
	sendKeysSettleDelay = 150 * time.Millisecond
	// maxCapturePaneErrors bounds how many consecutive capture-pane failures
	// WaitReady tolerates before giving up, so a session that died mid-startup
	// surfaces immediately instead of after the full timeout.
	maxCapturePaneErrors = 5
)

// readyGlyphs are part of Claude Code's persistent input-box chrome.
// The startup banner ("Claude Code") is intentionally not included — it is
// absent when Claude is launched with --continue or --resume.
var readyGlyphs = []string{"❯", "⏵"}

type Client struct{ Bin string }

func New() Client { return Client{Bin: "tmux"} }

func (c Client) run(args ...string) error {
	cmd := exec.Command(c.Bin, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("tmux %s: %w: %s", strings.Join(args, " "), err, stderr.String())
	}
	return nil
}

func (c Client) output(args ...string) (string, error) {
	cmd := exec.Command(c.Bin, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	b, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("tmux %s: %w: %s", strings.Join(args, " "), err, stderr.String())
	}
	return strings.TrimSpace(string(b)), nil
}

func (c Client) Has(name string) bool {
	return exec.Command(c.Bin, "has-session", "-t", name).Run() == nil
}

func (c Client) NewSession(name, cwd string, claudeArgs []string) error {
	args := []string{"new-session", "-d", "-s", name, "-c", cwd, "claude"}
	args = append(args, claudeArgs...)
	return c.run(args...)
}

func (c Client) WaitReady(name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	consecutiveErrs := 0
	for time.Now().Before(deadline) {
		// Capture the visible pane only (no -S scrollback) so a reused session
		// cannot match stale glyphs from prior output.
		out, err := c.output("capture-pane", "-pt", name)
		if err != nil {
			consecutiveErrs++
			if consecutiveErrs >= maxCapturePaneErrors {
				return fmt.Errorf("tmux capture-pane kept failing for session %s: %w", name, err)
			}
			time.Sleep(readyPollInterval)
			continue
		}
		consecutiveErrs = 0
		if paneIsReady(out) {
			return nil
		}
		time.Sleep(readyPollInterval)
	}
	return fmt.Errorf("timed out waiting for Claude input UI in tmux session %s", name)
}

func paneIsReady(pane string) bool {
	for _, g := range readyGlyphs {
		if !strings.Contains(pane, g) {
			return false
		}
	}
	return true
}

func (c Client) Attach(name string) error {
	cmd := exec.Command(c.Bin, "attach-session", "-t", name)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (c Client) Kill(name string) error { return c.run("kill-session", "-t", name) }

func (c Client) SendText(name, text string) error {
	// Type literal text instead of paste-buffer. Claude Code currently treats tmux
	// paste-buffer content as editable draft text but may not submit it on Enter in
	// detached sessions. send-keys -l follows the same path as normal typing.
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	parts := strings.Split(text, "\n")
	for i, part := range parts {
		if part != "" {
			if err := c.run("send-keys", "-t", name, "-l", part); err != nil {
				return err
			}
		}
		if i < len(parts)-1 {
			if err := c.run("send-keys", "-t", name, "C-j"); err != nil {
				return err
			}
		}
	}
	time.Sleep(sendKeysSettleDelay)
	return c.run("send-keys", "-t", name, "Enter")
}

func (c Client) Status(name string) string {
	if c.Has(name) {
		return "running"
	}
	return "dead"
}

func (c Client) PanePID(name string) string {
	out, _ := c.output("display-message", "-p", "-t", name, "#{pane_pid}")
	return out
}
