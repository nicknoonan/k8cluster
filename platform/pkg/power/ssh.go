package power

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"time"

	"golang.org/x/crypto/ssh"
)

type PowerOffClient interface {
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
	conn, err := ssh.Dial("tcp", net.JoinHostPort(target, "22"), c.config)
	if err != nil {
		return fmt.Errorf("dial ssh: %w", err)
	}
	defer conn.Close()

	session, err := conn.NewSession()
	if err != nil {
		return fmt.Errorf("create ssh session: %w", err)
	}
	defer session.Close()

	var stderr bytes.Buffer
	session.Stdout = &bytes.Buffer{}
	session.Stderr = &stderr

	if err := session.Run("sudo -n systemctl poweroff"); err != nil {
		return fmt.Errorf("execute poweroff on %s: %w: %s", target, err, stderr.String())
	}

	return nil
}
