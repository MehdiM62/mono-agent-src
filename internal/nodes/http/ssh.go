package httpnodes

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/monoes/mono-agent/internal/workflow"
)

// SSHNode runs a remote shell command over SSH, or (resource "file")
// uploads and downloads files over SFTP.
// Type: "http.ssh"
type SSHNode struct{}

func (n *SSHNode) Type() string { return "http.ssh" }

func (n *SSHNode) Execute(ctx context.Context, input workflow.NodeInput, config map[string]interface{}) ([]workflow.NodeOutput, error) {
	host, _ := config["host"].(string)
	if host == "" {
		return nil, fmt.Errorf("http.ssh: 'host' is required")
	}

	port := 22
	if v, ok := config["port"].(float64); ok {
		port = int(v)
	}

	username, _ := config["username"].(string)
	if username == "" {
		return nil, fmt.Errorf("http.ssh: 'username' is required")
	}

	resource, _ := config["resource"].(string)
	command, _ := config["command"].(string)
	switch resource {
	case "", "command":
		if command == "" {
			return nil, fmt.Errorf("http.ssh: 'command' is required")
		}
	case "file":
		if err := checkSFTPConfig(config); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("http.ssh: unknown resource %q (want command or file)", resource)
	}

	timeoutSecs := 30
	if v, ok := config["timeout_seconds"].(float64); ok {
		timeoutSecs = int(v)
	}
	timeout := time.Duration(timeoutSecs) * time.Second

	// Build auth methods
	var authMethods []ssh.AuthMethod

	privateKeyPEM, _ := config["private_key"].(string)
	passphrase, _ := config["passphrase"].(string)
	password, _ := config["password"].(string)

	if privateKeyPEM != "" {
		var signer ssh.Signer
		var err error
		if passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(privateKeyPEM), []byte(passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey([]byte(privateKeyPEM))
		}
		if err != nil {
			return nil, fmt.Errorf("http.ssh: failed to parse private key: %w", err)
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	}
	if password != "" {
		authMethods = append(authMethods, ssh.Password(password))
	}
	if len(authMethods) == 0 {
		return nil, fmt.Errorf("http.ssh: either 'password' or 'private_key' is required")
	}

	// Use known_hosts file for host key verification if provided; otherwise
	// fall back to InsecureIgnoreHostKey with a clear warning.
	var hostKeyCallback ssh.HostKeyCallback
	knownHostsPath, _ := config["known_hosts"].(string)
	if knownHostsPath != "" {
		if _, err := os.Stat(knownHostsPath); err != nil {
			return nil, fmt.Errorf("http.ssh: known_hosts file not accessible: %w", err)
		}
		cb, err := knownhosts.New(knownHostsPath)
		if err != nil {
			return nil, fmt.Errorf("http.ssh: failed to parse known_hosts: %w", err)
		}
		hostKeyCallback = cb
	} else {
		log.Println("WARNING: http.ssh: host key verification is disabled (InsecureIgnoreHostKey). " +
			"Set 'known_hosts' config field to a known_hosts file path to enable verification.")
		hostKeyCallback = ssh.InsecureIgnoreHostKey() //nolint:gosec
	}

	sshConfig := &ssh.ClientConfig{
		User:            username,
		Auth:            authMethods,
		HostKeyCallback: hostKeyCallback,
		Timeout:         timeout,
	}

	addr := fmt.Sprintf("%s:%d", host, port)

	// Dial with context
	dialer := &net.Dialer{Timeout: timeout}
	netConn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("http.ssh: dial failed: %w", err)
	}

	// The handshake waits for the server too (ClientConfig.Timeout covers only
	// ssh.Dial): the connection's deadline bounds it by the node's timeout,
	// and a cancelled context ends it.
	deadline := time.Now().Add(timeout)
	_ = netConn.SetDeadline(deadline)
	stopHandshake := context.AfterFunc(ctx, func() { _ = netConn.SetDeadline(time.Now()) })
	sshConn, chans, reqs, err := ssh.NewClientConn(netConn, addr, sshConfig)
	stopHandshake()
	if err != nil {
		netConn.Close()
		if ctx.Err() != nil {
			return nil, fmt.Errorf("http.ssh: cancelled: %w", ctx.Err())
		}
		if !time.Now().Before(deadline) {
			return nil, fmt.Errorf("http.ssh: timed out after %s waiting for the ssh handshake", timeout)
		}
		return nil, fmt.Errorf("http.ssh: handshake failed: %w", err)
	}
	_ = netConn.SetDeadline(time.Time{})
	client := ssh.NewClient(sshConn, chans, reqs)
	defer client.Close()

	if resource == "file" {
		return sftpFiles(ctx, client, timeout, input, config)
	}

	// The node's timeout and context hold from here: opening the session,
	// the environment request and the command each wait for the server.
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	session, err := awaitServer(ctx, client, timer, timeout, "open a session", client.NewSession)
	if err != nil {
		return nil, err
	}
	defer session.Close()

	// What a workflow runs is never the operator (workflow.EnvWorkflowRun),
	// over ssh too. sshd passes on only the variables AcceptEnv names, so on
	// this machine, where `monoagentcli hil approve` reaches the user's
	// database, the command exports the marker as well. Other hosts keep
	// their command as written: a Windows host's cmd.exe has no export.
	envDone := make(chan struct{})
	go func() {
		_ = session.Setenv(workflow.EnvWorkflowRun, workflow.RunMarker(input.ExecutionID))
		close(envDone)
	}()
	select {
	case <-envDone:
	case <-ctx.Done():
		session.Close()
		return nil, fmt.Errorf("http.ssh: cancelled: %w", ctx.Err())
	case <-timer.C:
		session.Close()
		return nil, fmt.Errorf("http.ssh: timed out after %s waiting for the server to answer an environment request", timeout)
	}
	if exportsMarker(ctx, host) {
		command = markedCommand(input.ExecutionID, command)
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	session.Stdout = &stdoutBuf
	session.Stderr = &stderrBuf

	// Run the command in a goroutine so we can honor the workflow context and
	// the configured timeout. session.Run itself has no deadline, so without
	// this a hung remote command would block the node forever and ignore
	// cancellation.
	runErrCh := make(chan error, 1)
	go func() { runErrCh <- session.Run(command) }()

	exitCode := 0
	var runErr error
	select {
	case runErr = <-runErrCh:
	case <-ctx.Done():
		session.Close()
		return nil, fmt.Errorf("http.ssh: cancelled: %w", ctx.Err())
	case <-timer.C:
		session.Close()
		return nil, fmt.Errorf("http.ssh: command timed out after %s", timeout)
	}
	if runErr != nil {
		if exitErr, ok := runErr.(*ssh.ExitError); ok {
			exitCode = exitErr.ExitStatus()
		} else {
			exitCode = -1
		}
	}

	resultItem := workflow.NewItem(map[string]interface{}{
		"stdout":    stdoutBuf.String(),
		"stderr":    stderrBuf.String(),
		"exit_code": exitCode,
	})

	handle := "main"
	if exitCode != 0 {
		handle = "error"
	}

	return []workflow.NodeOutput{{Handle: handle, Items: []workflow.Item{resultItem}}}, nil
}

// awaitServer runs open, a step that waits for the server, until the node's
// context ends or timer fires; then it closes client, which ends open, and
// says why.
func awaitServer[T any](ctx context.Context, client *ssh.Client, timer *time.Timer, timeout time.Duration, what string, open func() (T, error)) (T, error) {
	type result struct {
		v   T
		err error
	}
	done := make(chan result, 1)
	go func() {
		v, err := open()
		done <- result{v, err}
	}()
	var zero T
	select {
	case r := <-done:
		if r.err != nil {
			return zero, fmt.Errorf("http.ssh: could not %s: %w", what, r.err)
		}
		return r.v, nil
	case <-ctx.Done():
		client.Close()
		return zero, fmt.Errorf("http.ssh: cancelled: %w", ctx.Err())
	case <-timer.C:
		client.Close()
		return zero, fmt.Errorf("http.ssh: timed out after %s waiting for the server to %s", timeout, what)
	}
}

// markedCommand is command after an export of the run's marker
// (workflow.EnvWorkflowRun), single-quoted for the remote shell.
func markedCommand(executionID, command string) string {
	marker := "'" + strings.ReplaceAll(workflow.RunMarker(executionID), "'", `'\''`) + "'"
	return "export " + workflow.EnvWorkflowRun + "=" + marker + "; " + command
}
