package streams

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	jsoniter "github.com/json-iterator/go"
	"github.com/k3s-io/kine/pkg/util"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// performKubernetesDryRun sends the provided object/value to the Kubernetes API endpoint
// using server-side dry-run (dryRun=All). It understands a small subset of storage
// registry prefixes /registry/<resource>/... and maps them to the corresponding
// Kubernetes API resource paths. Unsupported keys or operations return an error so
// the message will be routed to DLQ by the caller.
func performKubernetesDryRun(ctx context.Context, kubeEndpoint string, key string, operation string, value []byte) error {
	if kubeEndpoint == "" {
		return errors.New("kubernetes API endpoint not configured")
	}

	// Try to decode value into Unstructured early so object metadata can be used
	var decodedObj *unstructured.Unstructured
	if len(value) > 0 {
		obj := &unstructured.Unstructured{}
		if _, _, err := unstructuredDecoder.Decode(value, nil, obj); err == nil {
			decodedObj = obj
		}
	}

	// Parse registry key and determine API path; prefer metadata from decoded object
	var apiPath string
	var name string
	var namespace string

	// Attempt to use decoded object's GVK to obtain canonical registry/api mapping
	var registryPrefix string
	var apiBase string
	var namespaced bool

	var resourcePlural string
	if decodedObj != nil {
		gvk := decodedObj.GroupVersionKind()
		registryPrefix, apiBase, namespaced = util.GetResourceMappingByGVK(gvk)
		// derive resource plural from registryPrefix when available
		if strings.HasPrefix(registryPrefix, "/registry/") {
			resourcePlural = strings.TrimSuffix(strings.TrimPrefix(registryPrefix, "/registry/"), "/")
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

		var gvk schema.GroupVersionKind
		if kg, ok := known[resourcePlural]; ok {
			gvk = kg
		} else {
			// best-effort: singularize and title-case the kind, assume core v1
			singular := resourcePlural
			if strings.HasSuffix(singular, "s") {
				singular = strings.TrimSuffix(singular, "s")
			}
			gvk = schema.GroupVersionKind{Group: "", Version: "v1", Kind: strings.Title(singular)}
		}

		registryPrefix, apiBase, namespaced = util.GetResourceMappingByGVK(gvk)
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
	trimPrefix := "/registry/" + resourcePlural + "/"
	rel := strings.TrimPrefix(key, trimPrefix)
	relParts := []string{}
	if rel != "" {
		relParts = strings.Split(rel, "/")
	}

	if namespaced {
		// expected: /registry/<resourcePlural>/{namespace}/{name}
		if namespace == "" {
			if len(relParts) < 2 || relParts[0] == "" {
				return fmt.Errorf("invalid %s key: %s", resourcePlural, key)
			}
			namespace = relParts[0]
		}
		if name == "" {
			if len(relParts) < 2 || relParts[1] == "" {
				return fmt.Errorf("invalid %s key: %s", resourcePlural, key)
			}
			name = relParts[1]
		}
		apiPath = strings.Replace(apiBase, "{namespace}", namespace, 1)
	} else {
		// cluster-scoped: expected: /registry/<resourcePlural>/{name}
		if name == "" {
			if len(relParts) < 1 || relParts[0] == "" {
				return fmt.Errorf("invalid %s key: %s", resourcePlural, key)
			}
			name = relParts[0]
		}
		apiPath = apiBase
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

	// Build URL
	u, err := url.Parse(kubeEndpoint)
	if err != nil {
		return fmt.Errorf("invalid kubernetes endpoint: %w", err)
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + apiPath

	// For update/delete operations, append the resource name to the path
	if operation == "update" || operation == "delete" {
		if name == "" {
			return fmt.Errorf("missing resource name for operation %s on key %s", operation, key)
		}
		u.Path = strings.TrimSuffix(u.Path, "/") + "/" + name
	}

	// Add dryRun=All query param
	q := u.Query()
	q.Set("dryRun", "All")
	u.RawQuery = q.Encode()
	// Prepare request body: try to decode into Unstructured and marshal to JSON for stability
	var bodyBytes []byte
	if operation == "create" || operation == "update" {
		if decodedObj != nil {
			if jb, err := jsoniter.Marshal(decodedObj); err == nil {
				bodyBytes = jb
			} else {
				bodyBytes = value
			}
		} else {
			obj := &unstructured.Unstructured{}
			if _, _, err := unstructuredDecoder.Decode(value, nil, obj); err != nil {
				// fallback: send raw bytes
				bodyBytes = value
			} else {
				if jb, err := jsoniter.Marshal(obj); err == nil {
					bodyBytes = jb
				} else {
					bodyBytes = value
				}
			}
		}
	}

	// Create HTTP request
	var req *http.Request
	if operation == "delete" {
		req, err = http.NewRequestWithContext(ctx, http.MethodDelete, u.String(), nil)
	} else if operation == "create" {
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(bodyBytes))
	} else if operation == "update" {
		req, err = http.NewRequestWithContext(ctx, http.MethodPut, u.String(), bytes.NewReader(bodyBytes))
	} else {
		return fmt.Errorf("unsupported operation for dry-run: %s", operation)
	}
	if err != nil {
		return err
	}

	req.Header.Set("Accept", "application/json")
	if operation == "create" || operation == "update" {
		req.Header.Set("Content-Type", "application/json")
	}

	// Small timeout to protect the consumer
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("kubernetes dry-run request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body for better error messages
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("kubernetes dry-run rejected: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	return nil
}
