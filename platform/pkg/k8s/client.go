package k8s

import (
	"context"
	"errors"
	"fmt"

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

func (c *Client) CordonAndDrain(ctx context.Context, nodeName string) error {
	if c == nil || c.client == nil {
		return errors.New("kubernetes client not configured")
	}

	if err := c.cordonNode(ctx, nodeName); err != nil {
		return err
	}

	pods, err := c.client.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{
		FieldSelector: "spec.nodeName=" + nodeName,
	})
	if err != nil {
		return err
	}

	for _, pod := range pods.Items {
		if isDaemonSetPod(&pod) {
			continue
		}

		eviction := &policyv1.Eviction{
			ObjectMeta: metav1.ObjectMeta{Name: pod.Name, Namespace: pod.Namespace},
			DeleteOptions: &metav1.DeleteOptions{
				GracePeriodSeconds: pointer64(30),
			},
		}
		if err := c.client.PolicyV1().Evictions(pod.Namespace).Evict(ctx, eviction); err != nil {
			return fmt.Errorf("evict pod %s/%s: %w", pod.Namespace, pod.Name, err)
		}
	}
	return nil
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
