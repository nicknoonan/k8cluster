package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseManagedDeployments(t *testing.T) {
	items, err := ParseManagedDeployments("default/api,media/transcoder")
	if err != nil {
		t.Fatalf("ParseManagedDeployments returned error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	if items[0].Namespace != "default" || items[0].Name != "api" {
		t.Fatalf("unexpected first item: %#v", items[0])
	}
}

func TestParseManagedDeploymentsRejectsInvalidValue(t *testing.T) {
	if _, err := ParseManagedDeployments("broken"); err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadSetsDefaultSSHShutdownCommand(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.SSHShutdownCommand != "sudo shutdown now" {
		t.Fatalf("SSHShutdownCommand = %q, want %q", cfg.SSHShutdownCommand, "sudo shutdown now")
	}
}

func TestLoadUsesConfiguredSSHShutdownCommand(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("SSH_SHUTDOWN_COMMAND", "shutdown -h now")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.SSHShutdownCommand != "shutdown -h now" {
		t.Fatalf("SSHShutdownCommand = %q, want %q", cfg.SSHShutdownCommand, "shutdown -h now")
	}
}

func setRequiredEnv(t *testing.T) {
	t.Helper()

	keyPath := filepath.Join(t.TempDir(), "id_rsa")
	if err := os.WriteFile(keyPath, []byte("test-key"), 0o600); err != nil {
		t.Fatalf("write temp key: %v", err)
	}

	t.Setenv("TARGET_NODE_NAME", "node")
	t.Setenv("TARGET_MAC_ADDRESS", "00:11:22:33:44:55")
	t.Setenv("TARGET_IP", "192.168.1.10")
	t.Setenv("SSH_PRIVATE_KEY_PATH", keyPath)
	t.Setenv("SSH_PRIVATE_KEY", "")
	t.Setenv("MANAGED_DEPLOYMENTS", "")
}
