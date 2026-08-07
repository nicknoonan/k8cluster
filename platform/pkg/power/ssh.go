package power

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

type PowerOffClient interface {
	Validate(ctx context.Context, target string) error
	PowerOff(ctx context.Context, target string) error
}

type SSHClient struct {
	user   string
	signer ssh.Signer
	config *ssh.ClientConfig
}

func NewSSHClient(user string, privateKey []byte) (*SSHClient, error) {
	signer, err := ssh.ParsePrivateKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("parse ssh private key: %w", err)
	}

	return &SSHClient{
		user:   user,
		signer: signer,
		config: &ssh.ClientConfig{
			User:            user,
			Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
			Timeout:         15 * time.Second,
		},
	}, nil
}

func (c *SSHClient) PowerOff(ctx context.Context, target string) error {
	return c.runCommand(ctx, target, shutdownCommand(c.user))
}

func (c *SSHClient) Validate(ctx context.Context, target string) error {
	return c.runCommand(ctx, target, validationCommand(c.user))
}

func (c *SSHClient) runCommand(ctx context.Context, target, command string) error {
	conn, err := ssh.Dial("tcp", net.JoinHostPort(target, "22"), c.config)
	if err != nil {
		return fmt.Errorf("dial ssh as %s: %w", c.user, err)
	}
	defer conn.Close()

	session, err := conn.NewSession()
	if err != nil {
		return fmt.Errorf("create ssh session as %s: %w", c.user, err)
	}
	defer session.Close()

	var stderr bytes.Buffer
	session.Stdout = &bytes.Buffer{}
	session.Stderr = &stderr

	if err := session.Run(command); err != nil {
		return fmt.Errorf("execute %q on %s as %s: %w: %s", command, target, c.user, err, stderr.String())
	}

	return nil
}

func shutdownCommand(user string) string {
	return privilegedShellCommand(user, "if command -v shutdown >/dev/null 2>&1; then shutdown now; else systemctl poweroff; fi")
}

func validationCommand(user string) string {
	return privilegedShellCommand(user, "if command -v shutdown >/dev/null 2>&1 || command -v systemctl >/dev/null 2>&1; then true; else exit 1; fi")
}

func privilegedShellCommand(user, script string) string {
	script = strings.ReplaceAll(script, `'`, `'\''`)
	if user == "root" {
		return fmt.Sprintf("sh -c '%s'", script)
	}
	return fmt.Sprintf("sudo -n sh -c '%s'", script)
}
