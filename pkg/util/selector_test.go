package util

import (
	"strings"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestEtcdKeyForGroupVersionKind_NilMappingReturnsError(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"}
	if _, err := etcdKeyForGroupVersionKind(gvk, "default", nil); err == nil {
		t.Fatal("expected error for nil mapping, got nil")
	}
}

func TestEtcdKeyForGroupVersionKind_CorePodNamespacedKey(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"}
	mapping := &meta.RESTMapping{
		Resource: schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
		Scope:    meta.RESTScopeNamespace,
	}
	key, err := etcdKeyForGroupVersionKind(gvk, "my-ns", mapping)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(key, "/registry/pods/") {
		t.Fatalf("expected key to contain registry pods prefix, got %q", key)
	}
	if !strings.Contains(key, "my-ns") {
		t.Fatalf("expected namespaced key to include namespace, got %q", key)
	}
}

func TestEtcdKeyForGroupVersionKind_AppsReplicaSetNamespacedKeyIncludesGroup(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "ReplicaSet"}
	mapping := &meta.RESTMapping{
		Resource: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "replicasets"},
		Scope:    meta.RESTScopeNamespace,
	}
	key, err := etcdKeyForGroupVersionKind(gvk, "foo", mapping)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(key, "/registry/apps/") {
		t.Fatalf("expected key to include group segment '/registry/apps/', got %q", key)
	}
	if !strings.Contains(key, "foo") {
		t.Fatalf("expected namespaced key to include namespace, got %q", key)
	}
}

func TestEtcdKeyForGroupVersionKind_NamespaceIsRootScoped_NoNamespaceInKey(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Namespace"}
	mapping := &meta.RESTMapping{
		Resource: schema.GroupVersionResource{Group: "", Version: "v1", Resource: "namespaces"},
		Scope:    meta.RESTScopeRoot,
	}
	key, err := etcdKeyForGroupVersionKind(gvk, "should-be-ignored", mapping)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(key, "/registry/namespaces/") {
		t.Fatalf("expected registry namespaces prefix in key, got %q", key)
	}
	if strings.Contains(key, "should-be-ignored") {
		t.Fatalf("expected root-scoped resource key to not include namespace, but it did: %q", key)
	}
}

// New tests to lock the desired RBAC etcd registry convention and object lookup
func TestGetResourceMappingByGVK_RBACClusterRole_UngroupedKey(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole"}
	registryPrefix, apiBase, namespaced := GetResourceMappingByGVK(gvk)
	if registryPrefix != "/registry/clusterroles/" {
		t.Fatalf("expected ungrouped registry prefix '/registry/clusterroles/', got %q (apiBase=%q)", registryPrefix, apiBase)
	}
	if namespaced {
		t.Fatalf("expected ClusterRole to be cluster-scoped (namespaced=false), got namespaced=%v", namespaced)
	}
}

func TestGetResourceMappingByGVK_RBACClusterRoleBinding_UngroupedKey(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRoleBinding"}
	registryPrefix, apiBase, namespaced := GetResourceMappingByGVK(gvk)
	if registryPrefix != "/registry/clusterrolebindings/" {
		t.Fatalf("expected ungrouped registry prefix '/registry/clusterrolebindings/', got %q (apiBase=%q)", registryPrefix, apiBase)
	}
	if namespaced {
		t.Fatalf("expected ClusterRoleBinding to be cluster-scoped (namespaced=false), got namespaced=%v", namespaced)
	}
}

func TestGetObjectByKey_ClusterRoleAndBindingKeysResolveToRBACObjects(t *testing.T) {
	obj := GetObjectByKey("/registry/clusterroles/foo")
	if _, ok := obj.(*rbacv1.ClusterRole); !ok {
		t.Fatalf("expected GetObjectByKey to return *rbacv1.ClusterRole for /registry/clusterroles/ key, got %T", obj)
	}

	obj = GetObjectByKey("/registry/clusterrolebindings/bar")
	if _, ok := obj.(*rbacv1.ClusterRoleBinding); !ok {
		t.Fatalf("expected GetObjectByKey to return *rbacv1.ClusterRoleBinding for /registry/clusterrolebindings/ key, got %T", obj)
	}
}
