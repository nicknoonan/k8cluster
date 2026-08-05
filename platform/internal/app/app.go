package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/nicknoonan/k8cluster/platform/pkg/config"
	"github.com/nicknoonan/k8cluster/platform/pkg/k8s"
	"github.com/nicknoonan/k8cluster/platform/pkg/power"
	"github.com/nicknoonan/k8cluster/platform/pkg/wol"
	staticassets "github.com/nicknoonan/k8cluster/platform/static"
)

type Dependencies struct {
	Config     config.Config
	Kubernetes *k8s.Client
	SSH        power.PowerOffClient
	WoL        wol.Sender
}

type App struct {
	cfg        config.Config
	kubernetes *k8s.Client
	ssh        power.PowerOffClient
	wol        wol.Sender
	router     *mux.Router
}

type statusResponse struct {
	Phase       string                         `json:"phase"`
	Node        k8s.NodeInfo                   `json:"node"`
	Deployments []config.ManagedDeploymentInfo `json:"deployments"`
}

func New(deps Dependencies) (*App, error) {
	if deps.WoL == nil {
		deps.WoL = wol.DefaultSender{}
	}

	app := &App{
		cfg:        deps.Config,
		kubernetes: deps.Kubernetes,
		ssh:        deps.SSH,
		wol:        deps.WoL,
		router:     mux.NewRouter(),
	}
	app.router.Use(app.loggingMiddleware)
	app.registerRoutes()
	return app, nil
}

func (a *App) Router() http.Handler {
	return a.router
}

func (a *App) registerRoutes() {
	a.router.HandleFunc("/api/status", a.handleStatus).Methods(http.MethodGet)
	a.router.HandleFunc("/api/power/on", a.handlePowerOn).Methods(http.MethodPost)
	a.router.HandleFunc("/api/power/off", a.handlePowerOff).Methods(http.MethodPost)

	a.router.PathPrefix("/").Handler(http.FileServer(http.FS(staticassets.FS)))
}

func (a *App) handleStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	node, err := a.kubernetes.NodeStatus(ctx, a.cfg.TargetNodeName)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}

	deployments, err := a.kubernetes.ManagedDeployments(ctx, a.cfg.ManagedDeployments)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}

	writeJSON(w, http.StatusOK, statusResponse{
		Phase:       derivePhase(node, deployments),
		Node:        node,
		Deployments: deployments,
	})
}

func (a *App) handlePowerOn(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), a.cfg.PowerOnTimeout)
	defer cancel()

	if err := a.wol.Send(ctx, a.cfg.TargetMacAddress); err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}

	readyCtx, readyCancel := context.WithTimeout(ctx, a.cfg.NodeReadyTimeout)
	defer readyCancel()
	if err := a.waitForNodeReady(readyCtx); err != nil {
		writeError(w, http.StatusGatewayTimeout, err)
		return
	}

	if err := a.kubernetes.SetManagedDeploymentsReplicas(ctx, a.cfg.ManagedDeployments, 1); err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]string{"status": "booting"})
}

func (a *App) handlePowerOff(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), a.cfg.PowerOffTimeout)
	defer cancel()

	if a.ssh == nil {
		writeError(w, http.StatusInternalServerError, errors.New("ssh client not configured"))
		return
	}

	if err := a.kubernetes.SetManagedDeploymentsReplicas(ctx, a.cfg.ManagedDeployments, 0); err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}

	if err := a.kubernetes.CordonAndDrain(ctx, a.cfg.TargetNodeName); err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}

	if err := a.ssh.PowerOff(ctx, a.cfg.TargetIP); err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]string{"status": "shutting-down"})
}

func (a *App) waitForNodeReady(ctx context.Context) error {
	ticker := time.NewTicker(a.cfg.NodePollInterval)
	defer ticker.Stop()

	for {
		node, err := a.kubernetes.NodeStatus(ctx, a.cfg.TargetNodeName)
		if err != nil {
			return err
		}
		if node.Ready {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func derivePhase(node k8s.NodeInfo, deployments []config.ManagedDeploymentInfo) string {
	if node.Ready {
		return "READY"
	}
	if node.Cordoned {
		return "DRAINING"
	}
	for _, deployment := range deployments {
		if deployment.Replicas > 0 {
			return "BOOTING"
		}
	}
	return "OFFLINE"
}

func (a *App) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
