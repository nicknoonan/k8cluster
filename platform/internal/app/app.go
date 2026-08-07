package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
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
	cfg         config.Config
	kubernetes  *k8s.Client
	ssh         power.PowerOffClient
	wol         wol.Sender
	router      *mux.Router
	operationMu sync.RWMutex
	operation   *operationStatus
}

type operationEvent struct {
	At      time.Time `json:"at"`
	Message string    `json:"message"`
}

type operationStatus struct {
	Kind        string           `json:"kind"`
	Phase       string           `json:"phase"`
	Message     string           `json:"message"`
	InProgress  bool             `json:"inProgress"`
	StartedAt   time.Time        `json:"startedAt"`
	UpdatedAt   time.Time        `json:"updatedAt"`
	CompletedAt *time.Time       `json:"completedAt,omitempty"`
	Error       string           `json:"error,omitempty"`
	Events      []operationEvent `json:"events"`
}

type statusResponse struct {
	Phase       string                         `json:"phase"`
	Node        k8s.NodeInfo                   `json:"node"`
	Deployments []config.ManagedDeploymentInfo `json:"deployments"`
	Operation   *operationStatus               `json:"operation,omitempty"`
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
		Operation:   a.operationSnapshot(),
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
	if a.ssh == nil {
		writeError(w, http.StatusInternalServerError, errors.New("ssh client not configured"))
		return
	}

	operation, err := a.startOperation("power-off", "QUEUED", fmt.Sprintf("Preparing to shut down node %s", a.cfg.TargetNodeName))
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}

	go a.runPowerOffOperation()

	writeJSON(w, http.StatusAccepted, operation)
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

func (a *App) runPowerOffOperation() {
	ctx, cancel := context.WithTimeout(context.Background(), a.cfg.PowerOffTimeout)
	defer cancel()

	a.transitionOperation("SCALING_DOWN", "Scaling managed deployments to zero")
	if err := a.kubernetes.SetManagedDeploymentsReplicas(ctx, a.cfg.ManagedDeployments, 0); err != nil {
		a.failOperation(fmt.Errorf("scale managed deployments: %w", err))
		return
	}

	a.transitionOperation("DRAINING", fmt.Sprintf("Cordoning and draining node %s", a.cfg.TargetNodeName))
	if err := a.kubernetes.CordonAndDrain(ctx, a.cfg.TargetNodeName, k8s.DrainOptions{
		GracePeriod:  30 * time.Second,
		PollInterval: 2 * time.Second,
		Reporter:     a.recordOperationEvent,
	}); err != nil {
		a.failOperation(fmt.Errorf("drain node %s: %w", a.cfg.TargetNodeName, err))
		return
	}

	a.transitionOperation("SHUTTING_DOWN", fmt.Sprintf("Sending shutdown request to %s", a.cfg.TargetIP))
	if err := a.ssh.PowerOff(ctx, a.cfg.TargetIP); err != nil {
		a.failOperation(fmt.Errorf("power off node %s: %w", a.cfg.TargetIP, err))
		return
	}

	a.completeOperation(fmt.Sprintf("Shutdown request sent to node %s", a.cfg.TargetNodeName))
}

func (a *App) startOperation(kind, phase, message string) (*operationStatus, error) {
	a.operationMu.Lock()
	defer a.operationMu.Unlock()

	if a.operation != nil && a.operation.InProgress {
		return nil, fmt.Errorf("%s operation already in progress", a.operation.Kind)
	}

	now := time.Now().UTC()
	a.operation = &operationStatus{
		Kind:       kind,
		Phase:      phase,
		Message:    message,
		InProgress: true,
		StartedAt:  now,
		UpdatedAt:  now,
		Events: []operationEvent{
			{At: now, Message: message},
		},
	}
	log.Printf("[%s] %s", kind, message)

	return cloneOperationStatus(a.operation), nil
}

func (a *App) transitionOperation(phase, message string) {
	a.updateOperation(phase, message, "")
}

func (a *App) recordOperationEvent(message string) {
	a.updateOperation("", message, "")
}

func (a *App) completeOperation(message string) {
	now := time.Now().UTC()
	a.updateOperation("COMPLETED", message, "")

	a.operationMu.Lock()
	defer a.operationMu.Unlock()

	if a.operation == nil {
		return
	}
	a.operation.InProgress = false
	a.operation.CompletedAt = &now
	a.operation.UpdatedAt = now
}

func (a *App) failOperation(err error) {
	now := time.Now().UTC()
	a.updateOperation("FAILED", err.Error(), err.Error())

	a.operationMu.Lock()
	defer a.operationMu.Unlock()

	if a.operation == nil {
		return
	}
	a.operation.InProgress = false
	a.operation.Error = err.Error()
	a.operation.CompletedAt = &now
	a.operation.UpdatedAt = now
}

func (a *App) updateOperation(phase, message, operationError string) {
	a.operationMu.Lock()
	defer a.operationMu.Unlock()

	if a.operation == nil {
		return
	}

	now := time.Now().UTC()
	if phase != "" {
		a.operation.Phase = phase
	}
	a.operation.Message = message
	a.operation.UpdatedAt = now
	if operationError != "" {
		a.operation.Error = operationError
	}
	a.operation.Events = append(a.operation.Events, operationEvent{
		At:      now,
		Message: message,
	})
	log.Printf("[%s] %s", a.operation.Kind, message)
}

func (a *App) operationSnapshot() *operationStatus {
	a.operationMu.RLock()
	defer a.operationMu.RUnlock()

	return cloneOperationStatus(a.operation)
}

func cloneOperationStatus(status *operationStatus) *operationStatus {
	if status == nil {
		return nil
	}

	clone := *status
	clone.Events = append([]operationEvent(nil), status.Events...)
	if status.CompletedAt != nil {
		completedAt := *status.CompletedAt
		clone.CompletedAt = &completedAt
	}
	return &clone
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
