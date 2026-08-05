package main

import (
	"log"
	"net/http"

	"github.com/nicknoonan/k8cluster/platform/internal/app"
	"github.com/nicknoonan/k8cluster/platform/pkg/config"
	"github.com/nicknoonan/k8cluster/platform/pkg/k8s"
	"github.com/nicknoonan/k8cluster/platform/pkg/power"
	"github.com/nicknoonan/k8cluster/platform/pkg/wol"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	k8sClient, err := k8s.New()
	if err != nil {
		log.Printf("kubernetes client unavailable: %v", err)
	}

	sshClient, err := power.NewSSHClient(cfg.SSHUser, cfg.SSHPrivateKey)
	if err != nil {
		log.Fatalf("load ssh client: %v", err)
	}

	server, err := app.New(app.Dependencies{
		Config:     cfg,
		Kubernetes: k8sClient,
		SSH:        sshClient,
		WoL:        wol.DefaultSender{},
	})
	if err != nil {
		log.Fatalf("construct app: %v", err)
	}

	addr := ":" + cfg.Port
	log.Printf("listening on %s", addr)
	if err := http.ListenAndServe(addr, server.Router()); err != nil && err != http.ErrServerClosed {
		log.Fatalln(err)
	}
}
