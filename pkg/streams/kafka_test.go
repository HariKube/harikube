package streams

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"testing"

	"github.com/k3s-io/kine/pkg/util"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// These tests target the small, pure seam that will be implemented in
// pkg/streams/kafka.go: a function that derives the concurrent reader
// count from the Kafka reader configuration (JSON). The production change
// will provide GetReaderCountFromConfig(configEnc string) (int, error) or
// equivalent; these tests assert the expected behavior:
// - configured readers == 0  -> defaults to 1
// - configured readers < 0   -> defaults to 1
// - configured readers == 3  -> returns 3

func mustBase64Config(t *testing.T, m map[string]interface{}) string {
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(b)
}

func TestGetReaderCountFromConfig_DefaultsWhenZero(t *testing.T) {
	cfg := map[string]interface{}{
		"brokers": []string{"127.0.0.1:9092"},
		"topic":   "test",
		"readers": 0,
	}
	enc := mustBase64Config(t, cfg)

	// Production will implement: GetReaderCountFromConfig(configEnc string) (int, error)
	count, err := GetReaderCountFromConfig(enc)
	if err != nil {
		t.Fatalf("GetReaderCountFromConfig returned error: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected reader count to default to 1 when configured 0, got %d", count)
	}
}

func TestGetReaderCountFromConfig_DefaultsWhenNegative(t *testing.T) {
	cfg := map[string]interface{}{
		"brokers": []string{"127.0.0.1:9092"},
		"topic":   "test",
		"readers": -5,
	}
	enc := mustBase64Config(t, cfg)

	count, err := GetReaderCountFromConfig(enc)
	if err != nil {
		t.Fatalf("GetReaderCountFromConfig returned error: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected reader count to default to 1 when configured negative, got %d", count)
	}
}

func TestGetReaderCountFromConfig_PreservesPositive(t *testing.T) {
	cfg := map[string]interface{}{
		"brokers": []string{"127.0.0.1:9092"},
		"topic":   "test",
		"readers": 3,
	}
	enc := mustBase64Config(t, cfg)

	count, err := GetReaderCountFromConfig(enc)
	if err != nil {
		t.Fatalf("GetReaderCountFromConfig returned error: %v", err)
	}
	if count != 3 {
		t.Fatalf("expected reader count to be preserved when configured positive (3), got %d", count)
	}
}

// The following test asserts the expected behavior for the consumer's
// dry-run validation path: when the Kubernetes client configuration cannot
// be created/loaded (for example an invalid kubeconfig path), the
// performKubernetesDryRun helper should return an error — and the consumer
// logic (StartKafkaConsumer) is expected to treat that as a validation
// failure that results in the message being routed to the DLQ rather than
// silently succeeding. This test focuses on the helper behavior; the
// consumer-level routing will be validated in the adjacent production
// change that wires DLQ behavior into StartKafkaConsumer.
func TestPerformKubernetesDryRun_InvalidKubeconfig_CausesError(t *testing.T) {
	obj := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata": map[string]interface{}{
			"name":      "test-pod",
			"namespace": "default",
		},
	}
	b, err := json.Marshal(obj)
	if err != nil {
		t.Fatal(err)
	}

	// Provide a path that's almost certainly not a valid kubeconfig; the
	// helper should return a non-nil error indicating the client/config
	// could not be created, which downstream consumer code must treat as a
	// validation failure and DLQ the message.
	err = performKubernetesDryRun(context.Background(), "/nonexistent/does-not-exist-kubeconfig", "/registry/pods/default/create", "create", b)
	if err == nil {
		t.Fatal("expected performKubernetesDryRun to return an error when kubeconfig cannot be loaded, got nil")
	}
}

// applyDefaultNamespace is a small test-local helper that encodes the
// namespace-defaulting policy that StartKafkaConsumer's create branch was
// observed to perform: only namespaced resources should receive a
// metadata.namespace="default" when missing; cluster-scoped resources must
// not be assigned a namespace.
func applyDefaultNamespace(obj *unstructured.Unstructured) {
	gvk := obj.GroupVersionKind()
	_, _, namespaced := util.GetResourceMappingByGVK(gvk)
	if namespaced {
		if obj.GetNamespace() == "" {
			obj.SetNamespace("default")
		}
	}
}

func TestDefaulting_DoesNotApplyToClusterScopedResources(t *testing.T) {
	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion("rbac.authorization.k8s.io/v1")
	obj.SetKind("ClusterRole")
	obj.SetName("test-clusterrole")
	// intentionally leave namespace empty

	applyDefaultNamespace(obj)

	if obj.GetNamespace() != "" {
		t.Fatalf("cluster-scoped resource ClusterRole should not receive a default namespace, got %q", obj.GetNamespace())
	}
}

func TestDefaulting_AppliesToNamespacedResources(t *testing.T) {
	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion("v1")
	obj.SetKind("ConfigMap")
	obj.SetName("test-cm")
	// intentionally leave namespace empty

	applyDefaultNamespace(obj)

	if obj.GetNamespace() != "default" {
		t.Fatalf("namespaced resource ConfigMap should receive default namespace 'default' when missing, got %q", obj.GetNamespace())
	}
}

// Tests for the env-driven Kafka security/dialer helper that will be
// implemented in pkg/streams/kafka.go. These tests exercise the various
// combinations of protocol, TLS, mTLS and SASL mechanisms the helper must
// support. They intentionally avoid network I/O and assert on the returned
// kafka.Dialer configuration (TLS presence, client certificates, SASL
// mechanism name) so the production change can concentrate on wiring the
// same helper into both consumer and producer paths.

func TestBuildKafkaDialer_PLAINTEXT(t *testing.T) {
	t.Setenv("KAFKA_PROTOCOL", "PLAINTEXT")
	d, err := BuildKafkaDialerFromEnv()
	if err != nil {
		t.Fatalf("expected no error for PLAINTEXT, got: %v", err)
	}
	if d == nil {
		t.Fatal("expected non-nil dialer for PLAINTEXT")
	}
	if d.TLS != nil {
		t.Fatal("expected no TLS for PLAINTEXT, got TLS config present")
	}
	if d.SASLMechanism != nil {
		t.Fatalf("expected no SASL mechanism for PLAINTEXT, got %T", d.SASLMechanism)
	}
}

func TestBuildKafkaDialer_SSL_ServerAuth(t *testing.T) {
	caFile, err := os.CreateTemp("", "kafka-ca-*.pem")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(caFile.Name())
	_ = os.WriteFile(caFile.Name(), []byte("dummy-ca"), 0644)

	t.Setenv("KAFKA_PROTOCOL", "SSL")
	t.Setenv("KAFKA_TLS_CA", caFile.Name())
	d, err := BuildKafkaDialerFromEnv()
	if err != nil {
		t.Fatalf("expected no error for SSL server-auth, got: %v", err)
	}
	if d.TLS == nil {
		t.Fatal("expected TLS config to be present for SSL protocol")
	}
	if len(d.TLS.Certificates) != 0 {
		t.Fatalf("expected no client certificates for server-auth-only SSL, got %d", len(d.TLS.Certificates))
	}
}

func TestBuildKafkaDialer_mTLS_ClientCert(t *testing.T) {
	// Create placeholder client cert/key files; production will parse real PEMs.
	certFile, err := os.CreateTemp("", "kafka-client-cert-*.pem")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(certFile.Name())
	_ = os.WriteFile(certFile.Name(), []byte("dummy-cert"), 0644)

	keyFile, err := os.CreateTemp("", "kafka-client-key-*.pem")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(keyFile.Name())
	_ = os.WriteFile(keyFile.Name(), []byte("dummy-key"), 0600)

	t.Setenv("KAFKA_PROTOCOL", "SSL")
	t.Setenv("KAFKA_TLS_CERT", certFile.Name())
	t.Setenv("KAFKA_TLS_KEY", keyFile.Name())
	d, err := BuildKafkaDialerFromEnv()
	if err != nil {
		t.Fatalf("expected no error for mTLS setup, got: %v", err)
	}
	if d.TLS == nil {
		t.Fatal("expected TLS config to be present for mTLS")
	}
	if len(d.TLS.Certificates) == 0 {
		t.Fatal("expected client certificate to be loaded for mTLS, got 0 certificates")
	}
}

func TestBuildKafkaDialer_SASL_SSL_PlainAndScram(t *testing.T) {
	// PLAIN
	t.Setenv("KAFKA_PROTOCOL", "SASL_SSL")
	t.Setenv("KAFKA_SASL_MECHANISM", "PLAIN")
	t.Setenv("KAFKA_SASL_USERNAME", "user")
	t.Setenv("KAFKA_SASL_PASSWORD", "pass")
	d, err := BuildKafkaDialerFromEnv()
	if err != nil {
		t.Fatalf("expected no error for SASL_SSL PLAIN, got: %v", err)
	}
	if d.SASLMechanism == nil {
		t.Fatal("expected SASL mechanism for PLAIN to be set")
	}
	if got := d.SASLMechanism.Name(); got != "PLAIN" {
		t.Fatalf("expected SASL mechanism name 'PLAIN', got %q", got)
	}

	// SCRAM-SHA-256
	t.Setenv("KAFKA_SASL_MECHANISM", "SCRAM-SHA-256")
	// ensure username/password still present
	if _, err := BuildKafkaDialerFromEnv(); err != nil {
		t.Fatalf("expected no error for SASL_SSL SCRAM-SHA-256, got: %v", err)
	}

	// SCRAM-SHA-512
	t.Setenv("KAFKA_SASL_MECHANISM", "SCRAM-SHA-512")
	if _, err := BuildKafkaDialerFromEnv(); err != nil {
		t.Fatalf("expected no error for SASL_SSL SCRAM-SHA-512, got: %v", err)
	}
}

func TestBuildKafkaDialer_InvalidProtocolOrMechanism(t *testing.T) {
	// Invalid protocol
	t.Setenv("KAFKA_PROTOCOL", "NO_SUCH_PROTOCOL")
	if _, err := BuildKafkaDialerFromEnv(); err == nil {
		t.Fatal("expected error for invalid protocol, got nil")
	}

	// Invalid mechanism
	t.Setenv("KAFKA_PROTOCOL", "SASL_SSL")
	t.Setenv("KAFKA_SASL_MECHANISM", "NO_SUCH_MECH")
	if _, err := BuildKafkaDialerFromEnv(); err == nil {
		t.Fatal("expected error for invalid SASL mechanism, got nil")
	}
}
