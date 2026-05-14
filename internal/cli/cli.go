package cli

import (
	"context"
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/melonamin/agentrun/internal/session"
	"github.com/melonamin/agentrun/internal/tmux"
	"github.com/melonamin/agentrun/internal/transcript"
	"github.com/spf13/cobra"
)

type opts struct {
	sessionID, name, cwd         string
	detach, textOut, quiet, last bool
	stream                       bool
	idle, timeout                time.Duration
}

var o opts
var jsonCompat bool
var helpPrintCompat bool
var helpOutputFormat string
var helpInputFormat string
var helpIncludePartial bool
var helpIncludeHooks bool
var helpReplayUser bool
var helpNoPersistence bool
var helpPersistSession bool

func Execute() {
	args := os.Args[1:]
	if shouldUseCobra(args) {
		if err := root().Execute(); err != nil {
			printCLIError(err)
			os.Exit(1)
		}
		return
	}
	cwd, _ := os.Getwd()
	opts, err := parsePromptArgs(args, cwd)
	if err == nil && len(opts.PromptArgs) == 0 && opts.InputFormat == inputText && !stdinIsPipe() {
		_ = root().Help()
		return
	}
	if err == nil {
		err = runPromptOptions(opts)
	}
	if err != nil {
		printCLIError(err)
		os.Exit(1)
	}
}

func printCLIError(err error) {
	var ce cliError
	if errors.As(err, &ce) && ce.ClaudeStyle {
		fmt.Fprintln(os.Stderr, ce.Error())
		return
	}
	fmt.Fprintln(os.Stderr, "agentrun:", err)
}

type cleanupManager struct {
	mu   sync.Mutex
	once sync.Once
	fn   func()
}

func (c *cleanupManager) Set(fn func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.fn = fn
}

func (c *cleanupManager) Empty() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.fn == nil
}

func (c *cleanupManager) Run() {
	c.once.Do(func() {
		c.mu.Lock()
		fn := c.fn
		c.mu.Unlock()
		if fn != nil {
			fn()
		}
	})
}

func cleanupSignalContext(cleanup *cleanupManager) (context.Context, func()) {
	ctx, cancel := context.WithCancel(context.Background())
	signals := make(chan os.Signal, 2)
	done := make(chan struct{})
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-done:
			return
		case <-signals:
			cancel()
			cleanup.Run()
		}
	}()
	return ctx, func() {
		signal.Stop(signals)
		close(done)
		cancel()
	}
}

func shouldUseCobra(args []string) bool {
	// Empty args deliberately fall through to the manual path so a TTY invocation
	// prints help instead of cobra's RunE blocking on os.Stdin.
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "help", "completion", "list", "status", "attach", "kill", "transcript", "--help", "-h":
		return true
	default:
		return false
	}
}

func root() *cobra.Command {
	o.cwd, _ = os.Getwd()
	o.idle = 2 * time.Second
	o.timeout = 30 * time.Minute
	cmd := &cobra.Command{Use: "agentrun [prompt]", Short: "Claude print-compatible CLI with optional persistent sessions", Args: cobra.ArbitraryArgs, RunE: func(cmd *cobra.Command, args []string) error {
		cwd, _ := os.Getwd()
		opts, err := parsePromptArgs(os.Args[1:], cwd)
		if err != nil {
			return err
		}
		return runPromptOptions(opts)
	}}
	addFlags(cmd)
	cmd.AddCommand(listCmd(), statusCmd(), attachCmd(), killCmd(), transcriptCmd())
	return cmd
}
func addFlags(c *cobra.Command) {
	f := c.Flags()
	f.StringVarP(&o.sessionID, "session", "s", "", "session id or name")
	f.BoolVar(&o.last, "last", false, "use most recent session")
	f.StringVar(&o.name, "name", "", "name for a new agentrun session")
	f.StringVar(&o.cwd, "cwd", o.cwd, "working directory for new session")
	f.BoolVarP(&o.detach, "detach", "d", false, "send prompt and return immediately")
	f.BoolVarP(&helpPrintCompat, "print", "p", false, "Claude -p compatibility mode")
	f.StringVar(&helpOutputFormat, "output-format", "", "print compatibility output: text, json, or stream-json")
	f.StringVar(&helpInputFormat, "input-format", "", "print compatibility input: text or stream-json")
	f.BoolVar(&helpIncludePartial, "include-partial-messages", false, "accept Claude -p partial-message streaming flag")
	f.BoolVar(&helpIncludeHooks, "include-hook-events", false, "accept Claude -p hook-event streaming flag")
	f.BoolVar(&helpReplayUser, "replay-user-messages", false, "echo stream-json user messages back to stdout")
	f.BoolVar(&helpNoPersistence, "no-session-persistence", false, "remove the tmux-backed session after the turn")
	f.BoolVar(&helpPersistSession, "persist-session", false, "use agentrun's tmux-backed persistent session mode")
	f.BoolVar(&o.stream, "stream", false, "stream JSON output; raw transcript events in persistent mode")
	f.BoolVar(&o.textOut, "text", false, "emit human-readable text instead of JSON")
	f.BoolVarP(&o.quiet, "quiet", "q", false, "print assistant text only")
	f.BoolVar(&jsonCompat, "json", false, "emit JSON (persistent default; kept for compatibility)")
	_ = f.MarkHidden("json")
	f.DurationVar(&o.idle, "idle-timeout", o.idle, "transcript stability duration used to detect turn completion")
	f.DurationVar(&o.timeout, "turn-timeout", o.timeout, "maximum time to wait for a turn")
}

func runPromptOptions(opts promptOptions) error {
	cleanup := &cleanupManager{}
	ctx, stopSignals := cleanupSignalContext(cleanup)
	defer stopSignals()
	defer cleanup.Run()
	if !opts.UsesTmuxSession() {
		return runDirectPrint(ctx, opts)
	}
	if opts.InputFormat == inputStreamJSON {
		messages, err := readStreamMessages(os.Stdin)
		if err != nil {
			return err
		}
		if len(messages) == 0 {
			return fmt.Errorf("--input-format stream-json requires user messages on stdin")
		}
		client, s, err := prepareSession(opts, cleanup)
		if err != nil {
			return err
		}
		for _, msg := range messages {
			if opts.ReplayUserMessages {
				_ = replayMessage(os.Stdout, msg.Raw)
			}
			if err := runTurn(ctx, opts, client, s, msg.Text); err != nil {
				return err
			}
		}
		return nil
	}
	prompt, err := readTextPrompt(opts.PromptArgs, os.Stdin)
	if err != nil {
		return err
	}
	if prompt == "" {
		return fmt.Errorf("prompt is required")
	}
	client, s, err := prepareSession(opts, cleanup)
	if err != nil {
		return err
	}
	return runTurn(ctx, opts, client, s, prompt)
}

func runDirectPrint(parent context.Context, opts promptOptions) error {
	ctx, cancel := context.WithTimeout(parent, opts.TurnTimeout)
	defer cancel()

	args := directClaudeArgs(opts)
	cmd := exec.Command("claude", args...)
	cmd.Dir = opts.CWD
	if len(opts.PromptArgs) == 0 {
		cmd.Stdin = os.Stdin
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return err
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		killProcessGroup(cmd.Process.Pid)
		err := <-done
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
}

func directClaudeArgs(opts promptOptions) []string {
	args := append([]string(nil), opts.ClaudeArgs...)
	args = append(args, "--print")
	if opts.OutputFormatExplicit {
		args = append(args, "--output-format", opts.OutputFormat)
	}
	if opts.InputFormat != inputText {
		args = append(args, "--input-format", opts.InputFormat)
	}
	if opts.IncludePartialMessages {
		args = append(args, "--include-partial-messages")
	}
	if opts.IncludeHookEvents {
		args = append(args, "--include-hook-events")
	}
	if opts.ReplayUserMessages {
		args = append(args, "--replay-user-messages")
	}
	args = append(args, opts.PromptArgs...)
	return args
}

func killProcessGroup(pid int) {
	if pid <= 0 {
		return
	}
	if err := syscall.Kill(-pid, syscall.SIGTERM); err == nil {
		time.Sleep(50 * time.Millisecond)
	}
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}

func prepareSession(opts promptOptions, cleanup *cleanupManager) (tmux.Client, *session.Session, error) {
	client := tmux.New()
	var s *session.Session
	if opts.Last || opts.SessionID != "" {
		reg, err := session.Load()
		if err != nil {
			return client, nil, err
		}
		if opts.Last {
			s, err = reg.Last()
		} else {
			s, err = reg.Get(opts.SessionID)
		}
		if err != nil {
			return client, nil, err
		}
	}
	if s != nil && len(opts.ClaudeArgs) > 0 {
		return client, nil, fmt.Errorf("Claude launch flags only apply when creating a new session; use a new session or omit them")
	}
	if s != nil && opts.NoSessionPersistence {
		return client, nil, fmt.Errorf("--no-session-persistence cannot be used with an existing session")
	}
	created := false
	if s == nil {
		if _, err := exec.LookPath("tmux"); err != nil {
			return client, nil, fmt.Errorf("tmux not found in PATH")
		}
		if _, err := exec.LookPath("claude"); err != nil {
			return client, nil, fmt.Errorf("claude not found in PATH")
		}
		now := time.Now()
		ns := session.Session{Name: opts.Name, CreatedAt: now, UpdatedAt: now, CWD: opts.CWD, ClaudeArgs: append([]string(nil), opts.ClaudeArgs...)}
		allocated, err := session.Allocate(ns)
		if err != nil {
			return client, nil, err
		}
		allocated.Tmux = tmuxSessionName(allocated.ID)
		if err := session.Update(allocated); err != nil {
			_ = session.Remove(allocated.ID)
			return client, nil, err
		}
		if err := client.NewSession(allocated.Tmux, allocated.CWD, allocated.ClaudeArgs); err != nil {
			_ = session.Remove(allocated.ID)
			return client, nil, err
		}
		s = &allocated
		created = true
		if opts.NoSessionPersistence {
			cleanup.Set(func() { _ = client.Kill(allocated.Tmux); _ = session.Remove(allocated.ID) })
		}
		if resumeID := resumeSessionID(allocated.ClaudeArgs); resumeID != "" {
			p := transcript.PathForSessionID(allocated.CWD, resumeID)
			if transcript.Size(p) > 0 {
				s.Transcript = p
				s.LastOffset = transcript.Size(p)
				if err := session.Update(*s); err != nil {
					return client, nil, err
				}
			}
		}
		// Wait until Claude's interactive input UI is ready before sending keystrokes.
		// If the prompt is typed during startup, Claude can show it in the draft box
		// without treating the trailing Enter as submit. Readiness detection is
		// best-effort (it depends on UI glyphs that can change across versions or
		// be absent on --continue/--resume), so on timeout we warn and proceed
		// rather than tearing down a freshly-allocated session.
		if err := client.WaitReady(allocated.Tmux, 20*time.Second); err != nil {
			fmt.Fprintf(os.Stderr, "agentrun: warning: %v; sending prompt anyway\n", err)
		}
		time.Sleep(1 * time.Second)
	}
	if opts.NoSessionPersistence && created && cleanup.Empty() {
		cleanup.Set(func() { _ = client.Kill(s.Tmux); _ = session.Remove(s.ID) })
	}
	return client, s, nil
}

func resumeSessionID(args []string) string {
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--resume", "-r":
			if i+1 < len(args) && args[i+1] != "" && args[i+1][0] != '-' {
				return args[i+1]
			}
		case "--continue", "-c":
			return ""
		default:
			const prefix = "--resume="
			if len(args[i]) > len(prefix) && args[i][:len(prefix)] == prefix {
				return args[i][len(prefix):]
			}
		}
	}
	return ""
}

func tmuxSessionName(id string) string {
	sum := sha1.Sum([]byte(session.StateDir()))
	return fmt.Sprintf("agentrun-%s-%x", id, sum[:4])
}

func runTurn(parent context.Context, opts promptOptions, client tmux.Client, s *session.Session, prompt string) error {
	if !client.Has(s.Tmux) {
		return fmt.Errorf("tmux session %s is not running", s.Tmux)
	}
	started := time.Now()
	offset := int64(0)
	if s.Transcript != "" {
		offset = transcript.Size(s.Transcript)
	}
	if err := client.SendText(s.Tmux, prompt); err != nil {
		return err
	}
	s.UpdatedAt = time.Now()
	if opts.Detach {
		ctx, cancel := context.WithTimeout(parent, detachedTranscriptTimeout(opts.TurnTimeout))
		defer cancel()
		if s.Transcript == "" {
			if p, err := waitTranscriptPath(ctx, s.CWD, started, prompt); err == nil {
				s.Transcript = p
			}
		}
		if s.Transcript != "" {
			s.LastOffset = transcript.Size(s.Transcript)
		}
		if err := session.Update(*s); err != nil {
			return err
		}
		return printDetach(opts, *s)
	}
	ctx, cancel := context.WithTimeout(parent, opts.TurnTimeout)
	defer cancel()
	if opts.OutputFormat == formatStreamJSON {
		res, err := transcript.StreamTurn(ctx, os.Stdout, s.Transcript, offset, s.CWD, started, opts.IdleTimeout, prompt, transcript.StreamOptions{IncludeHookEvents: opts.IncludeHookEvents, IncludePartialMessages: opts.IncludePartialMessages, EmitUserEvents: !opts.PrintCompat, Compat: opts.PrintCompat})
		if err != nil {
			return err
		}
		s.Transcript = res.Transcript
		s.LastOffset = res.Offset
		s.UpdatedAt = time.Now()
		if err := session.Update(*s); err != nil {
			return err
		}
		if opts.PrintCompat {
			return printStreamResult(opts, *s, res)
		}
		return nil
	}
	res, err := transcript.WaitTurn(ctx, s.Transcript, offset, s.CWD, started, opts.IdleTimeout, prompt)
	if err != nil {
		return err
	}
	s.Transcript = res.Transcript
	s.LastOffset = res.Offset
	s.UpdatedAt = time.Now()
	if err := session.Update(*s); err != nil {
		return err
	}
	return printResult(opts, *s, res)
}

func detachedTranscriptTimeout(turnTimeout time.Duration) time.Duration {
	if turnTimeout > 0 && turnTimeout < 30*time.Second {
		return turnTimeout
	}
	return 30 * time.Second
}

func waitTranscriptPath(ctx context.Context, cwd string, started time.Time, prompt string) (string, error) {
	for {
		var p string
		var err error
		if prompt != "" {
			p, err = transcript.FindChangedForPrompt(started, cwd, prompt)
		} else {
			p, err = transcript.FindChanged(started, cwd)
		}
		if err == nil {
			return p, nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func printDetach(opts promptOptions, s session.Session) error {
	if opts.Quiet || opts.OutputFormat == formatText {
		fmt.Println(s.ID)
		return nil
	}
	return encodeStdout(map[string]any{"session": s.ID, "status": "running", "tmux": s.Tmux, "cwd": s.CWD, "transcript": s.Transcript})
}
func printResult(opts promptOptions, s session.Session, r transcript.Result) error {
	if opts.OutputFormat == formatText || opts.Quiet {
		if r.Text != "" {
			fmt.Println(r.Text)
		}
		if !opts.PrintCompat && !opts.Quiet {
			fmt.Fprintf(os.Stderr, "Session: %s\n", s.ID)
		}
		return nil
	}
	if opts.PrintCompat {
		out := map[string]any{
			"type":               "result",
			"subtype":            "success",
			"is_error":           false,
			"api_error_status":   nil,
			"num_turns":          1,
			"result":             r.Text,
			"session_id":         transcript.SessionIDFromPath(r.Transcript),
			"terminal_reason":    "completed",
			"permission_denials": []any{},
			"agentrun": map[string]any{
				"session": s.ID, "tmux": s.Tmux, "cwd": s.CWD, "transcript": r.Transcript, "events": r.Events, "no_session_persistence": opts.NoSessionPersistence,
			},
		}
		return encodeStdout(out)
	}
	out := map[string]any{"session": s.ID, "status": "idle", "tmux": s.Tmux, "cwd": s.CWD, "transcript": r.Transcript, "result": map[string]any{"text": r.Text, "events": r.Events}}
	if opts.NoSessionPersistence {
		compat := map[string]any{"print": opts.PrintCompat, "no_session_persistence": opts.NoSessionPersistence}
		out["compat"] = compat
	}
	return encodeStdout(out)
}

func printStreamResult(opts promptOptions, s session.Session, r transcript.Result) error {
	out := map[string]any{
		"type":               "result",
		"subtype":            "success",
		"is_error":           false,
		"api_error_status":   nil,
		"num_turns":          1,
		"result":             r.Text,
		"session_id":         transcript.SessionIDFromPath(r.Transcript),
		"terminal_reason":    "completed",
		"permission_denials": []any{},
		"agentrun":           map[string]any{"session": s.ID, "tmux": s.Tmux, "cwd": s.CWD, "transcript": r.Transcript, "events": r.Events},
	}
	return encodeStdout(out)
}

func encodeStdout(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func listCmd() *cobra.Command {
	c := &cobra.Command{Use: "list", Short: "list sessions", RunE: func(cmd *cobra.Command, args []string) error {
		reg, err := session.Load()
		if err != nil {
			return err
		}
		client := tmux.New()
		if !o.textOut {
			rows := make([]map[string]any, 0, len(reg.Sessions))
			for _, s := range reg.Sessions {
				rows = append(rows, map[string]any{"session": s.ID, "name": s.Name, "status": client.Status(s.Tmux), "tmux": s.Tmux, "cwd": s.CWD, "transcript": s.Transcript, "claude_args": s.ClaudeArgs})
			}
			return json.NewEncoder(os.Stdout).Encode(map[string]any{"sessions": rows})
		}
		for _, s := range reg.Sessions {
			fmt.Printf("%s\t%s\t%s\t%s\t%s\t%v\n", s.ID, val(s.Name, "-"), client.Status(s.Tmux), s.CWD, s.Transcript, s.ClaudeArgs)
		}
		return nil
	}}
	addFlags(c)
	return c
}
func statusCmd() *cobra.Command {
	c := &cobra.Command{Use: "status", Short: "show session status", RunE: func(cmd *cobra.Command, args []string) error {
		s, err := getSess()
		if err != nil {
			return err
		}
		client := tmux.New()
		if !o.textOut {
			return json.NewEncoder(os.Stdout).Encode(map[string]any{"session": s.ID, "name": s.Name, "status": client.Status(s.Tmux), "tmux": s.Tmux, "cwd": s.CWD, "transcript": s.Transcript, "claude_args": s.ClaudeArgs})
		}
		fmt.Printf("session: %s\nstatus: %s\ntmux: %s\ncwd: %s\ntranscript: %s\nclaude_args: %v\n", s.ID, client.Status(s.Tmux), s.Tmux, s.CWD, s.Transcript, s.ClaudeArgs)
		return nil
	}}
	addFlags(c)
	return c
}
func attachCmd() *cobra.Command {
	c := &cobra.Command{Use: "attach", Short: "attach to tmux session", RunE: func(cmd *cobra.Command, args []string) error {
		s, err := getSess()
		if err != nil {
			return err
		}
		return tmux.New().Attach(s.Tmux)
	}}
	addFlags(c)
	return c
}
func killCmd() *cobra.Command {
	c := &cobra.Command{Use: "kill", Short: "kill tmux session", RunE: func(cmd *cobra.Command, args []string) error {
		s, err := getSess()
		if err != nil {
			return err
		}
		return tmux.New().Kill(s.Tmux)
	}}
	addFlags(c)
	return c
}
func transcriptCmd() *cobra.Command {
	c := &cobra.Command{Use: "transcript", Short: "print transcript path", RunE: func(cmd *cobra.Command, args []string) error {
		s, err := getSess()
		if err != nil {
			return err
		}
		fmt.Println(s.Transcript)
		return nil
	}}
	addFlags(c)
	return c
}
func getSess() (*session.Session, error) {
	reg, err := session.Load()
	if err != nil {
		return nil, err
	}
	if o.last {
		return reg.Last()
	}
	if o.sessionID == "" {
		return nil, fmt.Errorf("--session required")
	}
	return reg.Get(o.sessionID)
}
func val(v, d string) string {
	if v == "" {
		return d
	}
	return v
}
