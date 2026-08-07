package k8s

import (
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestCordonAndDrainSkipsDaemonSetsAndDeletesNode(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clientset := fake.NewSimpleClientset(
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}},
		newPod("default", "workload-pod", "node-1", nil),
		newPod("kube-system", "daemon-pod", "node-1", []metav1.OwnerReference{daemonSetOwnerReference("node-agent")}),
	)
	installEvictionReactor(clientset, func(_ context.Context, eviction *policyv1.Eviction) error {
		return clientset.Tracker().Delete(podsResource(), eviction.Namespace, eviction.Name)
	})

	client := &Client{client: clientset}
	var events []string

	err := client.CordonAndDrain(ctx, "node-1", DrainOptions{
		GracePeriod:  time.Second,
		PollInterval: time.Millisecond,
		Reporter: func(message string) {
			events = append(events, message)
		},
	})
	if err != nil {
		t.Fatalf("CordonAndDrain returned error: %v", err)
	}

	if _, err := clientset.CoreV1().Pods("default").Get(ctx, "workload-pod", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("expected workload pod to be removed, got err=%v", err)
	}
	if _, err := clientset.CoreV1().Pods("kube-system").Get(ctx, "daemon-pod", metav1.GetOptions{}); err != nil {
		t.Fatalf("expected daemonset pod to remain, got err=%v", err)
	}
	if _, err := clientset.CoreV1().Nodes().Get(ctx, "node-1", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("expected node to be deleted, got err=%v", err)
	}

	assertEventContains(t, events, "Skipping DaemonSet pod kube-system/daemon-pod")
	assertEventContains(t, events, "Deleted node node-1 from cluster")
}

func TestCordonAndDrainForceDeletesRemainingPodsAfterGrace(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clientset := fake.NewSimpleClientset(
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-2"}},
		newPod("default", "stuck-pod", "node-2", nil),
	)
	installEvictionReactor(clientset, func(context.Context, *policyv1.Eviction) error {
		return nil
	})

	client := &Client{client: clientset}
	var events []string

	err := client.CordonAndDrain(ctx, "node-2", DrainOptions{
		GracePeriod:  25 * time.Millisecond,
		PollInterval: 5 * time.Millisecond,
		Reporter: func(message string) {
			events = append(events, message)
		},
	})
	if err != nil {
		t.Fatalf("CordonAndDrain returned error: %v", err)
	}

	if _, err := clientset.CoreV1().Pods("default").Get(ctx, "stuck-pod", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("expected stuck pod to be force deleted, got err=%v", err)
	}
	if _, err := clientset.CoreV1().Nodes().Get(ctx, "node-2", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("expected node to be deleted, got err=%v", err)
	}

	assertEventContains(t, events, "Drain grace expired with 1 pod(s) still on node node-2")
	assertEventContains(t, events, "Force deleting pod default/stuck-pod")
}

func newPod(namespace, name, nodeName string, owners []metav1.OwnerReference) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       namespace,
			Name:            name,
			OwnerReferences: owners,
		},
		Spec: corev1.PodSpec{
			NodeName: nodeName,
		},
	}
}

func daemonSetOwnerReference(name string) metav1.OwnerReference {
	controller := true
	return metav1.OwnerReference{
		Kind:       "DaemonSet",
		Name:       name,
		Controller: &controller,
	}
}

func installEvictionReactor(clientset *fake.Clientset, reactor func(context.Context, *policyv1.Eviction) error) {
	clientset.PrependReactor("create", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() != "eviction" {
			return false, nil, nil
		}

		createAction, ok := action.(k8stesting.CreateAction)
		if !ok {
			return true, nil, nil
		}

		eviction, ok := createAction.GetObject().(*policyv1.Eviction)
		if !ok {
			return true, nil, nil
		}

		return true, &metav1.Status{Status: "Success"}, reactor(context.Background(), eviction)
	})
}

func podsResource() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Version:  "v1",
		Resource: "pods",
	}
}

func assertEventContains(t *testing.T, events []string, want string) {
	t.Helper()

	for _, event := range events {
		if strings.Contains(event, want) {
			return
		}
	}

	t.Fatalf("expected event containing %q, got %#v", want, events)
}
