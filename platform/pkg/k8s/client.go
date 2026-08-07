package k8s

import (
	"context"
	"errors"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"

	"github.com/nicknoonan/k8cluster/platform/pkg/config"
)

type Client struct {
	client kubernetes.Interface
}

type DrainOptions struct {
	GracePeriod  time.Duration
	PollInterval time.Duration
	Reporter     func(message string)
}

type NodeInfo struct {
	Name      string   `json:"name"`
	Ready     bool     `json:"ready"`
	Cordoned  bool     `json:"cordoned"`
	Exists    bool     `json:"exists"`
	Phase     string   `json:"phase"`
	Addresses []string `json:"addresses,omitempty"`
}

func New() (*Client, error) {
	restConfig, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, err
	}
	return &Client{client: clientset}, nil
}

func (c *Client) NodeStatus(ctx context.Context, nodeName string) (NodeInfo, error) {
	if c == nil || c.client == nil {
		return NodeInfo{}, errors.New("kubernetes client not configured")
	}

	node, err := c.client.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return NodeInfo{Name: nodeName, Exists: false, Phase: "MISSING"}, nil
		}
		return NodeInfo{}, err
	}

	info := NodeInfo{
		Name:     node.Name,
		Exists:   true,
		Cordoned: node.Spec.Unschedulable,
		Phase:    "NOTREADY",
	}

	for _, address := range node.Status.Addresses {
		info.Addresses = append(info.Addresses, address.Address)
	}

	for _, cond := range node.Status.Conditions {
		if cond.Type == corev1.NodeReady {
			info.Ready = cond.Status == corev1.ConditionTrue
			if info.Ready {
				info.Phase = "READY"
			}
		}
	}

	return info, nil
}

func (c *Client) ManagedDeployments(ctx context.Context, deployments []config.ManagedDeployment) ([]config.ManagedDeploymentInfo, error) {
	if c == nil || c.client == nil {
		return nil, errors.New("kubernetes client not configured")
	}

	result := make([]config.ManagedDeploymentInfo, 0, len(deployments))
	for _, deployment := range deployments {
		item, err := c.client.AppsV1().Deployments(deployment.Namespace).Get(ctx, deployment.Name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("get deployment %s/%s: %w", deployment.Namespace, deployment.Name, err)
		}
		result = append(result, config.ManagedDeploymentInfo{
			ManagedDeployment: deployment,
			Replicas:          replicas(item),
		})
	}
	return result, nil
}

func (c *Client) SetManagedDeploymentsReplicas(ctx context.Context, deployments []config.ManagedDeployment, replicas int32) error {
	if c == nil || c.client == nil {
		return errors.New("kubernetes client not configured")
	}

	for _, deployment := range deployments {
		if err := c.setDeploymentReplicas(ctx, deployment.Namespace, deployment.Name, replicas); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) setDeploymentReplicas(ctx context.Context, namespace, name string, replicas int32) error {
	deploymentClient := c.client.AppsV1().Deployments(namespace)
	deployment, err := deploymentClient.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get deployment %s/%s: %w", namespace, name, err)
	}
	deployment.Spec.Replicas = pointer(replicas)
	_, err = deploymentClient.Update(ctx, deployment, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("scale deployment %s/%s: %w", namespace, name, err)
	}
	return nil
}

func (c *Client) CordonAndDrain(ctx context.Context, nodeName string, options DrainOptions) error {
	if c == nil || c.client == nil {
		return errors.New("kubernetes client not configured")
	}

	gracePeriod := options.GracePeriod
	if gracePeriod <= 0 {
		gracePeriod = 30 * time.Second
	}

	pollInterval := options.PollInterval
	if pollInterval <= 0 {
		pollInterval = 2 * time.Second
	}

	if err := c.cordonNode(ctx, nodeName); err != nil {
		return err
	}
	reportDrainEvent(options.Reporter, "Cordoned node %s", nodeName)

	pods, err := c.client.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{
		FieldSelector: "spec.nodeName=" + nodeName,
	})
	if err != nil {
		return err
	}

	graceSeconds := int64(gracePeriod / time.Second)
	if graceSeconds < 1 {
		graceSeconds = 1
	}

	drainablePods := make([]corev1.Pod, 0, len(pods.Items))
	for _, pod := range pods.Items {
		if isDaemonSetPod(&pod) {
			reportDrainEvent(options.Reporter, "Skipping DaemonSet pod %s/%s", pod.Namespace, pod.Name)
			continue
		}
		drainablePods = append(drainablePods, pod)
	}

	if len(drainablePods) == 0 {
		reportDrainEvent(options.Reporter, "No drainable pods remain on node %s", nodeName)
	} else {
		reportDrainEvent(options.Reporter, "Evicting %d pod(s) from node %s with %d second grace", len(drainablePods), nodeName, graceSeconds)
	}

	for _, pod := range drainablePods {
		reportDrainEvent(options.Reporter, "Evicting pod %s/%s", pod.Namespace, pod.Name)
		eviction := &policyv1.Eviction{
			ObjectMeta: metav1.ObjectMeta{Name: pod.Name, Namespace: pod.Namespace},
			DeleteOptions: &metav1.DeleteOptions{
				GracePeriodSeconds: pointer64(graceSeconds),
			},
		}
		if err := c.client.PolicyV1().Evictions(pod.Namespace).Evict(ctx, eviction); err != nil {
			return fmt.Errorf("evict pod %s/%s: %w", pod.Namespace, pod.Name, err)
		}
	}

	remainingPods, err := c.waitForPodsToDrain(ctx, nodeName, gracePeriod, pollInterval, options.Reporter)
	if err != nil {
		return err
	}

	if len(remainingPods) > 0 {
		reportDrainEvent(options.Reporter, "Force deleting %d remaining pod(s) on node %s", len(remainingPods), nodeName)
		for _, pod := range remainingPods {
			reportDrainEvent(options.Reporter, "Force deleting pod %s/%s", pod.Namespace, pod.Name)
			if err := c.client.CoreV1().Pods(pod.Namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{
				GracePeriodSeconds: pointer64(0),
			}); err != nil && !apierrors.IsNotFound(err) {
				return fmt.Errorf("force delete pod %s/%s: %w", pod.Namespace, pod.Name, err)
			}
		}
	}

	reportDrainEvent(options.Reporter, "Deleting node %s from cluster", nodeName)
	if err := c.client.CoreV1().Nodes().Delete(ctx, nodeName, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete node %s: %w", nodeName, err)
	}
	reportDrainEvent(options.Reporter, "Deleted node %s from cluster", nodeName)
	return nil
}

func (c *Client) waitForPodsToDrain(ctx context.Context, nodeName string, gracePeriod, pollInterval time.Duration, reporter func(string)) ([]corev1.Pod, error) {
	deadline := time.Now().Add(gracePeriod)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		pods, err := c.listDrainablePods(ctx, nodeName)
		if err != nil {
			return nil, err
		}
		if len(pods) == 0 {
			reportDrainEvent(reporter, "All drainable pods removed from node %s", nodeName)
			return nil, nil
		}

		if time.Now().After(deadline) {
			reportDrainEvent(reporter, "Drain grace expired with %d pod(s) still on node %s", len(pods), nodeName)
			return pods, nil
		}

		reportDrainEvent(reporter, "Waiting for %d pod(s) to leave node %s", len(pods), nodeName)

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (c *Client) listDrainablePods(ctx context.Context, nodeName string) ([]corev1.Pod, error) {
	pods, err := c.client.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{
		FieldSelector: "spec.nodeName=" + nodeName,
	})
	if err != nil {
		return nil, err
	}

	drainablePods := make([]corev1.Pod, 0, len(pods.Items))
	for _, pod := range pods.Items {
		if isDaemonSetPod(&pod) {
			continue
		}
		drainablePods = append(drainablePods, pod)
	}

	return drainablePods, nil
}

func (c *Client) cordonNode(ctx context.Context, nodeName string) error {
	if c == nil || c.client == nil {
		return errors.New("kubernetes client not configured")
	}

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		node, err := c.client.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		node.Spec.Unschedulable = true
		_, err = c.client.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{})
		return err
	})
}

func replicas(deployment *appsv1.Deployment) int32 {
	if deployment.Spec.Replicas == nil {
		return 0
	}
	return *deployment.Spec.Replicas
}

func isDaemonSetPod(pod *corev1.Pod) bool {
	for _, owner := range pod.OwnerReferences {
		if owner.Controller != nil && *owner.Controller && owner.Kind == "DaemonSet" {
			return true
		}
	}
	return false
}

func pointer(value int32) *int32 { return &value }

func pointer64(value int64) *int64 { return &value }

func reportDrainEvent(reporter func(string), format string, args ...any) {
	if reporter == nil {
		return
	}
	reporter(fmt.Sprintf(format, args...))
}
