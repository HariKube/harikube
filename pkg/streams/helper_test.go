package streams

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// safeClose closes a channel and recovers from a panic if it was already closed.
// Used in tests to ensure blocked server handlers are unblocked even if the test
// exits early (e.g. via t.Fatalf) so httptest.Server.Close does not hang.
func safeClose(ch chan struct{}) {
	defer func() { _ = recover() }()
	close(ch)
}

func TestPerformKubernetesDryRun_LeaseOwnedByDifferent(t *testing.T) {
	// object with UID that will be used to look up Lease at namespace default
	obj := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata": map[string]interface{}{
			"name":      "test-pod",
			"namespace": "some-ns",
			"uid":       "obj-uid-123",
		},
	}
	b, err := json.Marshal(obj)
	if err != nil {
		t.Fatal(err)
	}

	// httptest server that returns a Lease owned by a different UID
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Lease GET path should be /apis/coordination.k8s.io/v1/namespaces/default/leases/<uid>
		if r.Method == http.MethodGet && r.URL.Path == "/apis/coordination.k8s.io/v1/namespaces/default/leases/obj-uid-123" {
			lease := map[string]interface{}{
				"apiVersion": "coordination.k8s.io/v1",
				"kind":       "Lease",
				"metadata": map[string]interface{}{
					"name":      "obj-uid-123",
					"namespace": "default",
					"ownerReferences": []map[string]interface{}{
						{
							"apiVersion": "v1",
							"kind":       "Pod",
							"name":       "other",
							"uid":        "different-uid",
						},
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(lease)
			return
		}

		// Any dry-run request should not be reached in this test scenario but return OK if it is
		if r.URL.Query().Get("dryRun") == "All" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(obj)
			return
		}

		// Default: not found
		http.NotFound(w, r)
	}))
	defer srv.Close()

	err = performKubernetesDryRun(context.Background(), srv.URL, "/registry/pods/some-ns/test-pod", "create", b)
	if err == nil {
		t.Fatalf("expected error due to lease owned by different object, got nil")
	}
}

func TestPerformKubernetesDryRun_LeaseMissingOrSameOwner(t *testing.T) {
	obj := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata": map[string]interface{}{
			"name":      "test-pod",
			"namespace": "some-ns",
			"uid":       "obj-uid-456",
		},
	}
	b, err := json.Marshal(obj)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("LeaseMissing_proceeds", func(t *testing.T) {
		// Server returns 404 for Lease and 200 for dry-run
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && r.URL.Path == "/apis/coordination.k8s.io/v1/namespaces/default/leases/obj-uid-456" {
				http.NotFound(w, r)
				return
			}
			if r.URL.Query().Get("dryRun") == "All" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(obj)
				return
			}
			http.NotFound(w, r)
		}))
		defer srv.Close()

		err := performKubernetesDryRun(context.Background(), srv.URL, "/registry/pods/some-ns/test-pod", "create", b)
		if err != nil {
			t.Fatalf("expected dry-run to proceed when lease missing, got error: %v", err)
		}
	})

	t.Run("LeaseSameOwner_proceeds", func(t *testing.T) {
		// Server returns a Lease whose owner UID matches the object's UID
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && r.URL.Path == "/apis/coordination.k8s.io/v1/namespaces/default/leases/obj-uid-456" {
				lease := map[string]interface{}{
					"apiVersion": "coordination.k8s.io/v1",
					"kind":       "Lease",
					"metadata": map[string]interface{}{
						"name":      "obj-uid-456",
						"namespace": "default",
						"ownerReferences": []map[string]interface{}{
							{
								"apiVersion": "v1",
								"kind":       "Pod",
								"name":       "test-pod",
								"uid":        "obj-uid-456",
							},
						},
					},
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(lease)
				return
			}
			if r.URL.Query().Get("dryRun") == "All" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(obj)
				return
			}
			http.NotFound(w, r)
		}))
		defer srv.Close()

		err := performKubernetesDryRun(context.Background(), srv.URL, "/registry/pods/some-ns/test-pod", "create", b)
		if err != nil {
			t.Fatalf("expected dry-run to proceed when lease owned by same UID, got error: %v", err)
		}
	})
}

func TestPerformKubernetesDryRun_ConcurrentLeaseAndDryRunStarted(t *testing.T) {
	obj := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata": map[string]interface{}{
			"name":      "test-pod",
			"namespace": "some-ns",
			"uid":       "obj-uid-456",
		},
	}
	b, err := json.Marshal(obj)
	if err != nil {
		t.Fatal(err)
	}

	leaseStarted := make(chan struct{}, 1)
	dryRunStarted := make(chan struct{}, 1)
	leaseProceed := make(chan struct{})
	dryRunProceed := make(chan struct{})
	// Ensure blocked handlers are released even if test exits early.
	defer safeClose(leaseProceed)
	defer safeClose(dryRunProceed)

	// Server that blocks the Lease GET until we allow it to proceed, and likewise for dry-run
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/apis/coordination.k8s.io/v1/namespaces/default/leases/obj-uid-456" {
			// signal that lease request has started
			t := leaseStarted
			select {
			case t <- struct{}{}:
			default:
			}
			<-leaseProceed
			// return NotFound like the LeaseMissing case
			http.NotFound(w, r)
			return
		}

		if r.URL.Query().Get("dryRun") == "All" {
			// signal that dry-run request has started
			s := dryRunStarted
			select {
			case s <- struct{}{}:
			default:
			}
			<-dryRunProceed
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(obj)
			return
		}

		http.NotFound(w, r)
	}))
	defer srv.Close()

	// Run performKubernetesDryRun in a goroutine since it will block until handlers proceed
	errCh := make(chan error, 1)
	go func() {
		errCh <- performKubernetesDryRun(context.Background(), srv.URL, "/registry/pods/some-ns/test-pod", "create", b)
	}()

	// Wait for lease request to start
	select {
	case <-leaseStarted:
		// started
	case <-time.After(2 * time.Second):
		t.Fatalf("lease request did not start in time")
	}

	// Now ensure the dry-run request is started while the lease request is still blocked.
	// If the code runs the lease check serially and waits for it to finish before starting the dry-run,
	// the dryRunStarted channel will not be signaled until after we allow leaseProceed, so this will time out.
	select {
	case <-dryRunStarted:
		// success: dry-run started while lease was still in-flight
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("dry-run request did not start while lease request was in-flight; requests may be serial")
	}

	// Allow both handlers to finish and collect result
	close(leaseProceed)
	close(dryRunProceed)

	if err := <-errCh; err != nil {
		t.Fatalf("expected dry-run to succeed, got error: %v", err)
	}
}

// TestPerformKubernetesDryRun_LoadsKubeconfig verifies that the helper path
// is expected to accept a kubeconfig file/path and use the native kubernetes
// client configuration within it (server URL) to perform the dry-run; this
// codifies the upcoming refactor where a kubeconfig is preferred over a raw
// API endpoint string. This test may fail until the production change is
// applied.
func TestPerformKubernetesDryRun_LoadsKubeconfig(t *testing.T) {
	obj := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata": map[string]interface{}{
			"name":      "test-pod",
			"namespace": "some-ns",
			"uid":       "obj-uid-789",
		},
	}
	b, err := json.Marshal(obj)
	if err != nil {
		t.Fatal(err)
	}

	// Server that responds to Lease GET and dry-run queries using the server URL
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/apis/coordination.k8s.io/v1/namespaces/default/leases/obj-uid-789" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("dryRun") == "All" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(obj)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	// Build a minimal kubeconfig that points to our test server as the cluster
	kcfg := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- cluster:
    server: %s
  name: test-cluster
contexts:
- context:
    cluster: test-cluster
    user: test-user
  name: test-context
current-context: test-context
users:
- name: test-user
  user:
    token: fake
`, srv.URL)

	tmpdir, err := os.MkdirTemp("", "kubeconf-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpdir)
	cfgPath := filepath.Join(tmpdir, "config")
	if err := os.WriteFile(cfgPath, []byte(kcfg), 0644); err != nil {
		t.Fatal(err)
	}

	// NOTE: the current performKubernetesDryRun signature takes a string; in
	// the refactor this will be interpreted as a kubeconfig path/content. Here
	// we pass the kubeconfig path to express the expected behavior.
	err = performKubernetesDryRun(context.Background(), cfgPath, "/registry/pods/some-ns/test-pod", "create", b)
	if err != nil {
		t.Fatalf("expected dry-run to succeed when kubeconfig points to server, got: %v", err)
	}
}

func TestPerformKubernetesDryRun_GroupedCRDRegistryKey(t *testing.T) {
	obj := map[string]interface{}{
		"apiVersion": "stable.example.com/v1",
		"kind":       "Shirt",
		"metadata": map[string]interface{}{
			"name":      "coming-on-kafka-stream",
			"namespace": "default",
		},
	}
	b, err := json.Marshal(obj)
	if err != nil {
		t.Fatal(err)
	}

	// Server that asserts the dry-run request targets the grouped CRD API path
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("dryRun") == "All" {
			if r.URL.Path != "/apis/stable.example.com/v1/namespaces/default/shirts" {
				t.Fatalf("expected dry-run to target grouped resource path, got %s", r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(obj)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	err = performKubernetesDryRun(context.Background(), srv.URL, "/registry/stable.example.com/shirts/default/coming-on-kafka-stream", "create", b)
	if err != nil {
		t.Fatalf("expected dry-run to succeed for grouped CRD, got: %v", err)
	}
}

func TestPerformKubernetesDryRun_EmptyEndpointUsesKubeconfigForGroupedCRD(t *testing.T) {
	obj := map[string]interface{}{
		"apiVersion": "stable.example.com/v1",
		"kind":       "Shirt",
		"metadata": map[string]interface{}{
			"name":      "coming-on-kafka-stream",
			"namespace": "default",
		},
	}
	b, err := json.Marshal(obj)
	if err != nil {
		t.Fatal(err)
	}

	// Server that asserts the dry-run request targets the grouped CRD API path
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("dryRun") == "All" {
			if r.URL.Path != "/apis/stable.example.com/v1/namespaces/default/shirts" {
				t.Fatalf("expected dry-run to target grouped resource path, got %s", r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(obj)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	// Build a minimal kubeconfig that points to our test server as the cluster
	kcfg := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- cluster:
    server: %s
  name: test-cluster
contexts:
- context:
    cluster: test-cluster
    user: test-user
  name: test-context
current-context: test-context
users:
- name: test-user
  user:
    token: fake
`, srv.URL)

	tmpdir, err := os.MkdirTemp("", "kubeconf-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpdir)
	cfgPath := filepath.Join(tmpdir, "config")
	if err := os.WriteFile(cfgPath, []byte(kcfg), 0644); err != nil {
		t.Fatal(err)
	}

	// Ensure KUBECONFIG env points to our kubeconfig and pass empty kubeEndpoint
	t.Setenv("KUBECONFIG", cfgPath)
	err = performKubernetesDryRun(context.Background(), "", "/registry/stable.example.com/shirts/default/coming-on-kafka-stream", "create", b)
	if err != nil {
		t.Fatalf("expected dry-run to succeed when KUBECONFIG points to server and kubeEndpoint is empty, got: %v", err)
	}
}

// TestPerformKubernetesDryRun_NoDefaultKubeconfigFallsBackToInCluster verifies
// that when kubeEndpoint is empty and KUBECONFIG is not set, an unreadable
// default $HOME/.kube/config does not prevent falling through to the
// in-cluster config path; the resulting error should reference the
// in-cluster configuration failure rather than solely complaining about the
// default kubeconfig file.
func TestPerformKubernetesDryRun_NoDefaultKubeconfigFallsBackToInCluster(t *testing.T) {
	obj := map[string]interface{}{
		"apiVersion": "stable.example.com/v1",
		"kind":       "Shirt",
		"metadata": map[string]interface{}{
			"name":      "coming-on-kafka-stream",
			"namespace": "default",
		},
	}
	b, err := json.Marshal(obj)
	if err != nil {
		t.Fatal(err)
	}

	// Ensure KUBECONFIG is unset for this test and restore it afterwards.
	prevKube, hadPrev := os.LookupEnv("KUBECONFIG")
	_ = os.Unsetenv("KUBECONFIG")
	if hadPrev {
		defer os.Setenv("KUBECONFIG", prevKube)
	}

	// Create a temporary HOME containing an unreadable default kubeconfig.
	tmpdir, err := os.MkdirTemp("", "no-default-kubeconfig")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpdir)
	kdir := filepath.Join(tmpdir, ".kube")
	if err := os.MkdirAll(kdir, 0700); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(kdir, "config")
	if err := os.WriteFile(cfgPath, []byte("not-a-valid-kubeconfig"), 0644); err != nil {
		t.Fatal(err)
	}
	// Make the file unreadable to simulate permission issues.
	if err := os.Chmod(cfgPath, 0000); err != nil {
		t.Fatal(err)
	}

	// Point HOME at the temp dir so client-go's default loading rules will look
	// for $HOME/.kube/config under our unreadable path.
	t.Setenv("HOME", tmpdir)

	// Execute with empty kubeEndpoint so the code tries kubeconfig then in-cluster.
	err = performKubernetesDryRun(context.Background(), "", "/registry/stable.example.com/shirts/default/coming-on-kafka-stream", "create", b)
	if err == nil {
		t.Fatalf("expected error when neither kubeconfig nor in-cluster config are available, got nil")
	}

	// The error should indicate an in-cluster config failure (fallback path),
	// not only a failure to load the default kubeconfig file.
	if !strings.Contains(err.Error(), "in-cluster") {
		t.Fatalf("expected error to reference in-cluster config, got: %v", err)
	}
	if strings.Contains(err.Error(), cfgPath) {
		t.Fatalf("expected error to not be about the unreadable default kubeconfig file path, but it referenced it: %v", err)
	}
}

// TestPerformKubernetesDryRun_EmptyKubeconfigFallsBackToInCluster asserts the
// intended behavior that when a kubeconfig is empty (or client configuration
// cannot be created), the helper should fail rather than silently succeeding
// so that the caller (e.g. Kafka consumer) can log and DLQ the message; this
// expectation may fail until the matching production change is applied.
func TestPerformKubernetesDryRun_EmptyKubeconfigFallsBackToInCluster(t *testing.T) {
	obj := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata": map[string]interface{}{
			"name":      "test-pod",
			"namespace": "some-ns",
			"uid":       "obj-uid-000",
		},
	}
	b, err := json.Marshal(obj)
	if err != nil {
		t.Fatal(err)
	}

	// We intentionally pass an empty string to represent an empty kubeconfig and
	// assert that performKubernetesDryRun returns an error when client config
	// creation fails, rather than treating this as a successful no-op.
	err = performKubernetesDryRun(context.Background(), "", "/registry/pods/some-ns/test-pod", "create", b)
	if err == nil {
		t.Fatalf("expected error when kubeconfig is empty and client config cannot be created, got nil")
	}
}

func TestPerformKubernetesDryRun_ConfigMapDryRunPath(t *testing.T) {
	obj := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]interface{}{
			"name":      "my-config",
			"namespace": "some-ns",
		},
	}
	b, err := json.Marshal(obj)
	if err != nil {
		t.Fatal(err)
	}

	// Server that asserts the dry-run request targets the namespaced ConfigMap API path
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("dryRun") == "All" {
			expected := "/api/v1/namespaces/some-ns/configmaps"
			if r.URL.Path != expected {
				t.Fatalf("expected dry-run to target ConfigMap path %s, got %s", expected, r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(obj)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	err = performKubernetesDryRun(context.Background(), srv.URL, "/registry/configmaps/some-ns/my-config", "create", b)
	if err != nil {
		t.Fatalf("expected dry-run to succeed for ConfigMap, got: %v", err)
	}
}

func TestPerformKubernetesDryRun_ClusterRoleDryRunPath(t *testing.T) {
	obj := map[string]interface{}{
		"apiVersion": "rbac.authorization.k8s.io/v1",
		"kind":       "ClusterRole",
		"metadata": map[string]interface{}{
			"name": "admin",
		},
	}
	b, err := json.Marshal(obj)
	if err != nil {
		t.Fatal(err)
	}

	// Server that asserts the dry-run request targets the cluster-scoped ClusterRole API path
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("dryRun") == "All" {
			expected := "/apis/rbac.authorization.k8s.io/v1/clusterroles"
			if r.URL.Path != expected {
				t.Fatalf("expected dry-run to target ClusterRole path %s, got %s", expected, r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(obj)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	err = performKubernetesDryRun(context.Background(), srv.URL, "/registry/clusterroles/admin", "create", b)
	if err != nil {
		t.Fatalf("expected dry-run to succeed for ClusterRole, got: %v", err)
	}
}
