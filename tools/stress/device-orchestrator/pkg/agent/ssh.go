package agent

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"

	"golang.org/x/crypto/ssh"
)

// SSHConfig describes how to reach the DUT and what to run on it.
type SSHConfig struct {
	// Host is the dial target, e.g. "10.0.0.1:22". The dialer expects a
	// host:port; callers should resolve hostnames upstream.
	Host string
	// User to authenticate as. Defaults to "admin" if empty.
	User string
	// KeyPath is the path to a PEM-encoded private key for public-key auth.
	KeyPath string
	// Command is the remote command to exec. Defaults to
	// "doublezero-agent -verbose" if empty; callers can override with
	// additional flags such as the controller address.
	Command string
	// LogPath, when non-empty, is the local file the SSH runner tees remote
	// stdout/stderr into. The file is truncated on Start.
	LogPath string
	// Logger is used for diagnostic logs from the runner; pass nil for silent.
	Logger *slog.Logger
}

// SSH is a Runner that dials the DUT over SSH, executes doublezero-agent in
// verbose mode, and emits AgentEvents parsed from the remote log stream.
//
// Host key verification uses ssh.InsecureIgnoreHostKey because the
// orchestrator targets ephemeral cEOS containers whose host keys regenerate
// on every restart; the threat model is "operator on the same subnet" and
// the SSH session carries no privileged credentials beyond what the keypair
// already grants. Do not reuse this dialer for production workloads.
type SSH struct {
	cfg SSHConfig

	events chan Event

	mu      sync.Mutex
	started bool
	client  *ssh.Client
	session *ssh.Session
	logFile *os.File
}

// NewSSH returns an unstarted SSH runner. Call Start to dial.
func NewSSH(cfg SSHConfig) *SSH {
	if cfg.User == "" {
		cfg.User = "admin"
	}
	if cfg.Command == "" {
		cfg.Command = "doublezero-agent -verbose"
	}
	return &SSH{
		cfg:    cfg,
		events: make(chan Event, 64),
	}
}

// Events returns the channel the runner emits AgentEvents on. It closes
// when the runner exits (ctx cancel, process exit, or session error).
func (s *SSH) Events() <-chan Event { return s.events }

// Start dials the DUT, opens a session, executes the configured command, and
// streams its stdout/stderr through the parser. Start returns once the
// session has been opened; the read loop runs in a goroutine until ctx is
// cancelled or the remote command exits.
func (s *SSH) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return fmt.Errorf("ssh agent: already started")
	}
	s.started = true
	s.mu.Unlock()

	signer, err := loadSigner(s.cfg.KeyPath)
	if err != nil {
		return fmt.Errorf("ssh agent: load key %s: %w", s.cfg.KeyPath, err)
	}

	clientCfg := &ssh.ClientConfig{
		User:            s.cfg.User,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
	client, err := ssh.Dial("tcp", s.cfg.Host, clientCfg)
	if err != nil {
		return fmt.Errorf("ssh agent: dial %s: %w", s.cfg.Host, err)
	}
	session, err := client.NewSession()
	if err != nil {
		_ = client.Close()
		return fmt.Errorf("ssh agent: new session: %w", err)
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		_ = session.Close()
		_ = client.Close()
		return fmt.Errorf("ssh agent: stdout pipe: %w", err)
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		_ = session.Close()
		_ = client.Close()
		return fmt.Errorf("ssh agent: stderr pipe: %w", err)
	}

	var logFile *os.File
	if s.cfg.LogPath != "" {
		logFile, err = os.Create(s.cfg.LogPath)
		if err != nil {
			_ = session.Close()
			_ = client.Close()
			return fmt.Errorf("ssh agent: open log %s: %w", s.cfg.LogPath, err)
		}
	}

	s.mu.Lock()
	s.client = client
	s.session = session
	s.logFile = logFile
	s.mu.Unlock()

	if err := session.Start(s.cfg.Command); err != nil {
		s.shutdown()
		return fmt.Errorf("ssh agent: start %q: %w", s.cfg.Command, err)
	}
	if s.cfg.Logger != nil {
		s.cfg.Logger.Info("ssh agent started", "host", s.cfg.Host, "command", s.cfg.Command, "log_path", s.cfg.LogPath)
	}

	// Funnel both streams through a single consumer. The two reader goroutines
	// only scan raw lines and forward them on `lines`; one consumer tees to the
	// log and runs the parser. This keeps the parser (which is not
	// goroutine-safe) and the log file touched by exactly one goroutine, while
	// still interleaving stdout/stderr in arrival order so a "Committing..."
	// marker and its multi-line diff body are parsed as one ordered sequence.
	parser := NewParser()
	lines := make(chan string, 256)

	var readWG sync.WaitGroup
	readWG.Add(2)
	go func() {
		defer readWG.Done()
		scanLines(ctx, stdout, lines, s.cfg.Logger, "stdout")
	}()
	go func() {
		defer readWG.Done()
		scanLines(ctx, stderr, lines, s.cfg.Logger, "stderr")
	}()
	go func() {
		readWG.Wait()
		close(lines)
	}()

	consumerDone := make(chan struct{})
	go func() {
		defer close(consumerDone)
		for line := range lines {
			if logFile != nil {
				if _, err := logFile.WriteString(line + "\n"); err != nil && s.cfg.Logger != nil {
					s.cfg.Logger.Warn("ssh agent: log tee write failed", "err", err)
				}
			}
			for _, ev := range parser.Parse(line) {
				select {
				case s.events <- ev:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	go func() {
		// Close session and channel when ctx cancels OR the streams end.
		select {
		case <-ctx.Done():
		case <-consumerDone:
		}
		// Closing the session causes the read loops to return EOF; the wait
		// below blocks until the consumer has drained before closing the events
		// channel, so consumers never see a half-emitted event.
		s.shutdown()
		<-consumerDone
		close(s.events)
	}()

	return nil
}

// shutdown is idempotent; safe to call from Start error paths and from the
// supervising goroutine.
func (s *SSH) shutdown() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session != nil {
		_ = s.session.Close()
		s.session = nil
	}
	if s.client != nil {
		_ = s.client.Close()
		s.client = nil
	}
	if s.logFile != nil {
		_ = s.logFile.Close()
		s.logFile = nil
	}
}

// scanLines reads `src` line-by-line and forwards each raw line on `lines`.
// It returns early when ctx cancels so a slow consumer can't deadlock
// shutdown. Tee-to-log and parsing happen in the single consumer that drains
// `lines`, so this reader never touches the parser or the log file.
func scanLines(ctx context.Context, src io.Reader, lines chan<- string, log *slog.Logger, label string) {
	scanner := bufio.NewScanner(src)
	scanner.Buffer(make([]byte, 1024*1024), 16*1024*1024) // large diffs can exceed default
	for scanner.Scan() {
		select {
		case lines <- scanner.Text():
		case <-ctx.Done():
			return
		}
	}
	if err := scanner.Err(); err != nil && log != nil {
		log.Warn("ssh agent: stream ended with error", "err", err, "stream", label)
	}
}

// loadSigner reads a PEM-encoded private key from disk and returns an ssh.Signer.
func loadSigner(path string) (ssh.Signer, error) {
	buf, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ssh.ParsePrivateKey(buf)
}
