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
	// decode early (best-effort)
	decodedObj := decodeUnstructured(value)

	// determine resource mapping and GVK (may inspect decoded object or fallback to key)
	resourcePlural, registryPrefix, namespaced, gvk, err := determineResourceAndGVK(decodedObj, key)
	if err != nil {
		return err
	}

	// resolve name/namespace (prefer decoded metadata, fallback to parsing the key)
	name, namespace, err := resolveNameNamespace(decodedObj, resourcePlural, registryPrefix, namespaced, key)
	if err != nil {
		return err
	}

	// build REST config
	cfg, err := buildRestConfig(kubeEndpoint)
	if err != nil {
		return err
	}

	// create dynamic client
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to create dynamic client: %w", err)
	}

	// prepare GVR and object to send
	gvr := schema.GroupVersionResource{Group: gvk.Group, Version: gvk.Version, Resource: resourcePlural}
	objToSend, err := prepareObjectToSend(operation, decodedObj, value)
	if err != nil {
		return err
	}

	// Run lease check and dry-run in parallel and combine results.
	leaseErrCh := runLeaseCheckChan(ctx, dyn, decodedObj)
	dryErrCh := runDryRunChan(ctx, dyn, gvr, namespaced, namespace, operation, objToSend, name)

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

// decodeUnstructured attempts to decode the provided bytes into an Unstructured object.
// It returns nil if decoding fails (preserving original best-effort semantics).
func decodeUnstructured(value []byte) *unstructured.Unstructured {
	if len(value) == 0 {
		return nil
	}
	obj := &unstructured.Unstructured{}
	if _, _, err := unstructuredDecoder.Decode(value, nil, obj); err != nil {
		return nil
	}
	return obj
}

// determineResourceAndGVK returns resourcePlural, registryPrefix, namespaced, gvk, error
func determineResourceAndGVK(decodedObj *unstructured.Unstructured, key string) (string, string, bool, schema.GroupVersionKind, error) {
	var resourcePlural string
	var registryPrefix string
	var namespaced bool
	var gvk schema.GroupVersionKind

	if decodedObj != nil {
		gvk = decodedObj.GroupVersionKind()
		registryPrefix, _, namespaced = util.GetResourceMappingByGVK(gvk)
		if strings.HasPrefix(registryPrefix, "/registry/") {
			raw := strings.TrimSuffix(strings.TrimPrefix(registryPrefix, "/registry/"), "/")
			if parts := strings.Split(raw, "/"); len(parts) > 1 {
				resourcePlural = parts[len(parts)-1]
			} else {
				resourcePlural = raw
			}
		}
		return resourcePlural, registryPrefix, namespaced, gvk, nil
	}

	// Fallback path: infer from key
	if !strings.HasPrefix(key, "/registry/") {
		return "", "", false, schema.GroupVersionKind{}, fmt.Errorf("unsupported registry key for dry-run: %s", key)
	}
	remaining := strings.TrimPrefix(key, "/registry/")
	parts := strings.Split(remaining, "/")
	if len(parts) == 0 || parts[0] == "" {
		return "", "", false, schema.GroupVersionKind{}, fmt.Errorf("unsupported registry key for dry-run: %s", key)
	}
	resourcePlural = parts[0]

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
		// best-effort
		singular := resourcePlural
		if before, ok0 := strings.CutSuffix(singular, "s"); ok0 {
			singular = before
		}
		gvk = schema.GroupVersionKind{Group: "", Version: "v1", Kind: strings.Title(singular)}
	}

	registryPrefix, _, namespaced = util.GetResourceMappingByGVK(gvk)
	return resourcePlural, registryPrefix, namespaced, gvk, nil
}

// resolveNameNamespace uses decoded metadata when present, otherwise parses from key based on mapping
func resolveNameNamespace(decodedObj *unstructured.Unstructured, resourcePlural, registryPrefix string, namespaced bool, key string) (string, string, error) {
	var name, namespace string
	if decodedObj != nil {
		if decodedObj.GetName() != "" {
			name = decodedObj.GetName()
		}
		if decodedObj.GetNamespace() != "" {
			namespace = decodedObj.GetNamespace()
		}
	}

	if resourcePlural == "" {
		if !strings.HasPrefix(key, "/registry/") {
			return "", "", fmt.Errorf("unsupported registry key for dry-run: %s", key)
		}
		resourcePlural = strings.Split(strings.TrimPrefix(key, "/registry/"), "/")[0]
	}

	var trimPrefix string
	if registryPrefix != "" && strings.HasPrefix(registryPrefix, "/registry/") {
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
		if namespace == "" {
			if len(relParts) < 2 || relParts[0] == "" {
				return "", "", fmt.Errorf("invalid %s key, namespace is missing for namespaced: %s", resourcePlural, key)
			}
			namespace = relParts[0]
		}
		if name == "" {
			if len(relParts) < 2 || relParts[1] == "" {
				return "", "", fmt.Errorf("invalid %s key, name is missing for namespaced: %s", resourcePlural, key)
			}
			name = relParts[1]
		}
	} else {
		if name == "" {
			if len(relParts) < 1 || relParts[0] == "" {
				return "", "", fmt.Errorf("invalid %s key, name is missing for clustered: %s", resourcePlural, key)
			}
			name = relParts[0]
		}
	}

	// prefer decoded metadata again
	if decodedObj != nil {
		if decodedObj.GetName() != "" {
			name = decodedObj.GetName()
		}
		if decodedObj.GetNamespace() != "" {
			namespace = decodedObj.GetNamespace()
		}
	}

	return name, namespace, nil
}

// buildRestConfig constructs a rest.Config from kubeEndpoint following the same fallback semantics
func buildRestConfig(kubeEndpoint string) (*rest.Config, error) {
	if kubeEndpoint == "" {
		if kubeEnv, ok := os.LookupEnv("KUBECONFIG"); ok && kubeEnv != "" {
			cfg2, err := clientcmd.BuildConfigFromFlags("", kubeEnv)
			if err != nil {
				return nil, fmt.Errorf("failed to build kubeconfig from %s: %w", kubeEnv, err)
			}
			return cfg2, nil
		}
		c2, err2 := rest.InClusterConfig()
		if err2 != nil {
			return nil, fmt.Errorf("failed to create in-cluster config: %w", err2)
		}
		return c2, nil
	}

	if fi, err := os.Stat(kubeEndpoint); err == nil && !fi.IsDir() {
		cfg2, err := clientcmd.BuildConfigFromFlags("", kubeEndpoint)
		if err != nil {
			return nil, fmt.Errorf("failed to build kubeconfig from %s: %w", kubeEndpoint, err)
		}
		return cfg2, nil
	}

	return &rest.Config{Host: kubeEndpoint, Timeout: 10 * time.Second}, nil
}

// prepareObjectToSend returns the object to use for create/update operations when applicable
func prepareObjectToSend(operation string, decodedObj *unstructured.Unstructured, value []byte) (*unstructured.Unstructured, error) {
	if operation != "create" && operation != "update" {
		return nil, nil
	}
	if decodedObj != nil {
		return decodedObj.DeepCopy(), nil
	}
	obj := &unstructured.Unstructured{}
	if _, _, err := unstructuredDecoder.Decode(value, nil, obj); err != nil {
		return nil, fmt.Errorf("failed to decode object for dry-run: %w", err)
	}
	return obj, nil
}

// runLeaseCheckChan executes the lease ownership check in a goroutine and returns a channel with the result
func runLeaseCheckChan(ctx context.Context, dyn dynamic.Interface, decodedObj *unstructured.Unstructured) chan error {
	ch := make(chan error, 1)
	if decodedObj != nil && decodedObj.GetUID() != "" {
		uid := string(decodedObj.GetUID())
		go func() {
			leaseRes := dyn.Resource(schema.GroupVersionResource{Group: "coordination.k8s.io", Version: "v1", Resource: "leases"}).Namespace("default")
			leaseObj, err := leaseRes.Get(ctx, uid, metav1.GetOptions{})
			if err != nil {
				if apierrors.IsNotFound(err) {
					ch <- nil
					return
				}
				ch <- fmt.Errorf("kubernetes lease check failed: %w", err)
				return
			}
			ors := leaseObj.GetOwnerReferences()
			if len(ors) == 0 {
				ch <- nil
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
				ch <- errors.New("kubernetes lease owned by different object")
				return
			}
			ch <- nil
		}()
	} else {
		go func() { ch <- nil }()
	}
	return ch
}

// runDryRunChan executes the dry-run request in a goroutine and returns a channel with the result
func runDryRunChan(ctx context.Context, dyn dynamic.Interface, gvr schema.GroupVersionResource, namespaced bool, namespace string, operation string, objToSend *unstructured.Unstructured, name string) chan error {
	ch := make(chan error, 1)
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
				ch <- fmt.Errorf("kubernetes dry-run request failed: %w", err)
				return
			}
		case "update":
			if _, err := ri.Update(ctx, objToSend, metav1.UpdateOptions{DryRun: []string{"All"}}); err != nil {
				ch <- fmt.Errorf("kubernetes dry-run request failed: %w", err)
				return
			}
		case "delete":
			if err := ri.Delete(ctx, name, metav1.DeleteOptions{DryRun: []string{"All"}}); err != nil {
				ch <- fmt.Errorf("kubernetes dry-run request failed: %w", err)
				return
			}
		default:
			ch <- fmt.Errorf("unsupported operation for dry-run: %s", operation)
			return
		}
		ch <- nil
	}()
	return ch
}
