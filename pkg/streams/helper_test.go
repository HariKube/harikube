package streams

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

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
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "ok")
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
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, "ok")
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
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, "ok")
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
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "ok")
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
