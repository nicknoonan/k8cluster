package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

type ManagedDeployment struct {
	Namespace string
	Name      string
}

type ManagedDeploymentInfo struct {
	ManagedDeployment
	Replicas int32 `json:"replicas"`
}

type Config struct {
	Port               string
	TargetNodeName     string
	TargetMacAddress   string
	TargetIP           string
	SSHUser            string
	SSHPrivateKey      []byte
	SSHPrivateKeyPath  string
	ManagedDeployments []ManagedDeployment
	PowerOnTimeout     time.Duration
	PowerOffTimeout    time.Duration
	NodeReadyTimeout   time.Duration
	NodePollInterval   time.Duration
}

func Load() (Config, error) {
	powerOnTimeout, err := durationOrDefault("POWER_ON_TIMEOUT", 10*time.Minute)
	if err != nil {
		return Config{}, err
	}
	powerOffTimeout, err := durationOrDefault("POWER_OFF_TIMEOUT", 5*time.Minute)
	if err != nil {
		return Config{}, err
	}
	nodeReadyTimeout, err := durationOrDefault("NODE_READY_TIMEOUT", 10*time.Minute)
	if err != nil {
		return Config{}, err
	}
	nodePollInterval, err := durationOrDefault("NODE_POLL_INTERVAL", 10*time.Second)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		Port:              envOrDefault("PORT", "8080"),
		TargetNodeName:    os.Getenv("TARGET_NODE_NAME"),
		TargetMacAddress:  os.Getenv("TARGET_MAC_ADDRESS"),
		TargetIP:          os.Getenv("TARGET_IP"),
		SSHUser:           envOrDefault("SSH_USER", "root"),
		SSHPrivateKeyPath: envOrDefault("SSH_PRIVATE_KEY_PATH", "/etc/ssh-keys/id_rsa"),
		PowerOnTimeout:    powerOnTimeout,
		PowerOffTimeout:   powerOffTimeout,
		NodeReadyTimeout:  nodeReadyTimeout,
		NodePollInterval:  nodePollInterval,
	}

	if deps := os.Getenv("MANAGED_DEPLOYMENTS"); deps != "" {
		parsed, err := ParseManagedDeployments(deps)
		if err != nil {
			return Config{}, err
		}
		cfg.ManagedDeployments = parsed
	}

	if cfg.TargetNodeName == "" || cfg.TargetMacAddress == "" || cfg.TargetIP == "" {
		return Config{}, errors.New("TARGET_NODE_NAME, TARGET_MAC_ADDRESS, and TARGET_IP are required")
	}

	if key := os.Getenv("SSH_PRIVATE_KEY"); key != "" {
		cfg.SSHPrivateKey = []byte(key)
	} else {
		data, err := os.ReadFile(cfg.SSHPrivateKeyPath)
		if err != nil {
			return Config{}, fmt.Errorf("read ssh private key: %w", err)
		}
		cfg.SSHPrivateKey = data
	}

	return cfg, nil
}

func ParseManagedDeployments(value string) ([]ManagedDeployment, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}

	parts := strings.Split(value, ",")
	deployments := make([]ManagedDeployment, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		namespace, name, ok := strings.Cut(trimmed, "/")
		if !ok || namespace == "" || name == "" {
			return nil, fmt.Errorf("managed deployment must be namespace/name: %q", trimmed)
		}
		deployments = append(deployments, ManagedDeployment{Namespace: namespace, Name: name})
	}
	return deployments, nil
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func durationOrDefault(key string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	return parsed, nil
}
