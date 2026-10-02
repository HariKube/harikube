package streams

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/k3s-io/kine/pkg/util"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// performKubernetesDryRun sends the provided object/value to the Kubernetes API endpoint
// using server-side dry-run (dryRun=All). It understands a small subset of storage
// registry prefixes /registry/<resource>/... and maps them to the corresponding
// Kubernetes API resource paths. Unsupported keys or operations return an error so
// the message will be routed to DLQ by the caller.
func performKubernetesDryRun(ctx context.Context, kubeEndpoint string, key string, operation string, value []byte) error {
	// Try to decode value into Unstructured early so object metadata can be used
	var decodedObj *unstructured.Unstructured
	if len(value) > 0 {
		obj := &unstructured.Unstructured{}
		if _, _, err := unstructuredDecoder.Decode(value, nil, obj); err == nil {
			decodedObj = obj
		}
	}

	// Parse registry key and determine API path; prefer metadata from decoded object
	var name string
	var namespace string

	// Attempt to use decoded object's GVK to obtain canonical registry/api mapping
	var registryPrefix string
	var namespaced bool

	var resourcePlural string
	var gvk schema.GroupVersionKind
	if decodedObj != nil {
		gvk = decodedObj.GroupVersionKind()
		registryPrefix, _, namespaced = util.GetResourceMappingByGVK(gvk)
		// derive resource plural from registryPrefix when available; for grouped CRDs
		// registryPrefix may be like "/registry/stable.example.com/shirts" — strip optional group segment
		if strings.HasPrefix(registryPrefix, "/registry/") {
			raw := strings.TrimSuffix(strings.TrimPrefix(registryPrefix, "/registry/"), "/")
			// If the mapping contains a group segment (e.g. "stable.example.com/shirts"), use only the resource plural ("shirts")
			if parts := strings.Split(raw, "/"); len(parts) > 1 {
				resourcePlural = parts[len(parts)-1]
			} else {
				resourcePlural = raw
			}
		}
	} else {
		// Fallback: infer resource plural from the registry key and map to a best-effort GVK
		if !strings.HasPrefix(key, "/registry/") {
			return fmt.Errorf("unsupported registry key for dry-run: %s", key)
		}
		remaining := strings.TrimPrefix(key, "/registry/")
		parts := strings.Split(remaining, "/")
		if len(parts) == 0 || parts[0] == "" {
			return fmt.Errorf("unsupported registry key for dry-run: %s", key)
		}
		resourcePlural = parts[0]

		// Known special-cases with non-default group/version/kind mappings
		known := map[string]schema.GroupVersionKind{
			"pods":                       {Group: "", Version: "v1", Kind: "Pod"},
			"events":                     {Group: "", Version: "v1", Kind: "Event"},
			"secrets":                    {Group: "", Version: "v1", Kind: "Secret"},
			"namespaces":                 {Group: "", Version: "v1", Kind: "Namespace"},
			"replicasets":                {Group: "apps", Version: "v1", Kind: "ReplicaSet"},
			"replicationcontrollers":     {Group: "", Version: "v1", Kind: "ReplicationController"},
			"jobs":                       {Group: "batch", Version: "v1", Kind: "Job"},
			"minions":                    {Group: "", Version: "v1", Kind: "Node"},
			"certificatesigningrequests": {Group: "certificates.k8s.io", Version: "v1", Kind: "CertificateSigningRequest"},
		}

		if kg, ok := known[resourcePlural]; ok {
			gvk = kg
		} else {
			// best-effort: singularize and title-case the kind, assume core v1
			singular := resourcePlural
			if before, ok0 := strings.CutSuffix(singular, "s"); ok0 {
				singular = before
			}
			gvk = schema.GroupVersionKind{Group: "", Version: "v1", Kind: strings.Title(singular)}
		}

		registryPrefix, _, namespaced = util.GetResourceMappingByGVK(gvk)
	}

	// Prefer metadata from decoded object for name/namespace when available
	if decodedObj != nil {
		if decodedObj.GetName() != "" {
			name = decodedObj.GetName()
		}
		if decodedObj.GetNamespace() != "" {
			namespace = decodedObj.GetNamespace()
		}
	}

	// Parse name/namespace from key as a fallback
	if resourcePlural == "" {
		// Last-ditch: attempt to extract from key directly
		if !strings.HasPrefix(key, "/registry/") {
			return fmt.Errorf("unsupported registry key for dry-run: %s", key)
		}
		resourcePlural = strings.Split(strings.TrimPrefix(key, "/registry/"), "/")[0]
	}
	// strip leading/trailing slashes and get the trailing path parts after the resource plural
	var trimPrefix string
	if registryPrefix != "" && strings.HasPrefix(registryPrefix, "/registry/") {
		// Use the canonical registry prefix returned by util when available (preserves group segment for matching the key)
		trimPrefix = strings.TrimSuffix(registryPrefix, "/") + "/"
	} else {
		trimPrefix = "/registry/" + resourcePlural + "/"
	}
	rel := strings.TrimPrefix(key, trimPrefix)
	relParts := []string{}
	if rel != "" {
		relParts = strings.Split(rel, "/")
	}

	if namespaced {
		// expected: /registry/<resourcePlural>/{namespace}/{name}
		if namespace == "" {
			if len(relParts) < 2 || relParts[0] == "" {
				return fmt.Errorf("invalid %s key, namespace is missing for namespaced: %s", resourcePlural, key)
			}
			namespace = relParts[0]
		}
		if name == "" {
			if len(relParts) < 2 || relParts[1] == "" {
				return fmt.Errorf("invalid %s key, name is missing for namespaced: %s", resourcePlural, key)
			}
			name = relParts[1]
		}
	} else {
		// cluster-scoped: expected: /registry/<resourcePlural>/{name}
		if name == "" {
			if len(relParts) < 1 || relParts[0] == "" {
				return fmt.Errorf("invalid %s key, name is missing for clustered: %s", resourcePlural, key)
			}
			name = relParts[0]
		}
	}

	// If object metadata provided name/namespace, we prefer those values
	if decodedObj != nil {
		if decodedObj.GetName() != "" {
			name = decodedObj.GetName()
		}
		if decodedObj.GetNamespace() != "" {
			namespace = decodedObj.GetNamespace()
		}
	}

	// Build REST config from kubeEndpoint: file path (kubeconfig) -> load, URL -> use as host, empty -> use KUBECONFIG only if explicitly set, otherwise fall back to in-cluster
	var cfg *rest.Config
	if kubeEndpoint == "" {
		// If KUBECONFIG is explicitly provided in the environment, use that file only.
		if kubeEnv, ok := os.LookupEnv("KUBECONFIG"); ok && kubeEnv != "" {
			cfg2, err := clientcmd.BuildConfigFromFlags("", kubeEnv)
			if err != nil {
				return fmt.Errorf("failed to build kubeconfig from %s: %w", kubeEnv, err)
			}
			cfg = cfg2
		} else {
			// No explicit KUBECONFIG: skip probing defaults (e.g. ~/.kube/config) and go straight to in-cluster
			c2, err2 := rest.InClusterConfig()
			if err2 != nil {
				return fmt.Errorf("failed to create in-cluster config: %w", err2)
			}
			cfg = c2
		}
	} else {
		// detect file path
		if fi, err := os.Stat(kubeEndpoint); err == nil && !fi.IsDir() {
			cfg2, err := clientcmd.BuildConfigFromFlags("", kubeEndpoint)
			if err != nil {
				return fmt.Errorf("failed to build kubeconfig from %s: %w", kubeEndpoint, err)
			}
			cfg = cfg2
		} else {
			// treat as direct API server host
			cfg = &rest.Config{Host: kubeEndpoint, Timeout: 10 * time.Second}
		}
	}

	// Ensure we have a usable config
	if cfg == nil {
		return errors.New("no usable kubernetes client config could be created")
	}

	// Create dynamic client
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to create dynamic client: %w", err)
	}

	// Prepare GroupVersionResource
	gvr := schema.GroupVersionResource{Group: gvk.Group, Version: gvk.Version, Resource: resourcePlural}

	// Prepare object for create/update
	var objToSend *unstructured.Unstructured
	if operation == "create" || operation == "update" {
		if decodedObj != nil {
			objToSend = decodedObj.DeepCopy()
		} else {
			obj := &unstructured.Unstructured{}
			if _, _, err := unstructuredDecoder.Decode(value, nil, obj); err != nil {
				return fmt.Errorf("failed to decode object for dry-run: %w", err)
			}
			objToSend = obj
		}
	}

	// Run lease check and dry-run in parallel and combine results.
	leaseErrCh := make(chan error, 1)
	dryErrCh := make(chan error, 1)

	// Lease goroutine
	if decodedObj != nil && decodedObj.GetUID() != "" {
		uid := string(decodedObj.GetUID())
		go func() {
			// perform a namespaced Lease GET against the Coordination API using the default namespace
			leaseRes := dyn.Resource(schema.GroupVersionResource{Group: "coordination.k8s.io", Version: "v1", Resource: "leases"}).Namespace("default")
			leaseObj, err := leaseRes.Get(ctx, uid, metav1.GetOptions{})
			if err != nil {
				if apierrors.IsNotFound(err) {
					leaseErrCh <- nil
					return
				}
				leaseErrCh <- fmt.Errorf("kubernetes lease check failed: %w", err)
				return
			}
			// Check ownerReferences
			ors := leaseObj.GetOwnerReferences()
			if len(ors) == 0 {
				leaseErrCh <- nil
				return
			}
			match := false
			for _, or := range ors {
				if string(or.UID) == uid {
					match = true
					break
				}
			}
			if !match {
				leaseErrCh <- fmt.Errorf("kubernetes lease owned by different object")
				return
			}
			leaseErrCh <- nil
		}()
	} else {
		go func() { leaseErrCh <- nil }()
	}

	// Dry-run goroutine
	go func() {
		var ri dynamic.ResourceInterface
		if namespaced {
			ri = dyn.Resource(gvr).Namespace(namespace)
		} else {
			ri = dyn.Resource(gvr)
		}

		switch operation {
		case "create":
			if _, err := ri.Create(ctx, objToSend, metav1.CreateOptions{DryRun: []string{"All"}}); err != nil {
				dryErrCh <- fmt.Errorf("kubernetes dry-run request failed: %w", err)
				return
			}
		case "update":
			if _, err := ri.Update(ctx, objToSend, metav1.UpdateOptions{DryRun: []string{"All"}}); err != nil {
				dryErrCh <- fmt.Errorf("kubernetes dry-run request failed: %w", err)
				return
			}
		case "delete":
			if err := ri.Delete(ctx, name, metav1.DeleteOptions{DryRun: []string{"All"}}); err != nil {
				dryErrCh <- fmt.Errorf("kubernetes dry-run request failed: %w", err)
				return
			}
		default:
			dryErrCh <- fmt.Errorf("unsupported operation for dry-run: %s", operation)
			return
		}
		dryErrCh <- nil
	}()

	// Wait for both results and combine: lease errors take precedence
	leaseErr := <-leaseErrCh
	dryErr := <-dryErrCh

	if leaseErr != nil {
		return leaseErr
	}
	if dryErr != nil {
		return dryErr
	}

	return nil
}
