package streams

import (
	"encoding/base64"
	"encoding/json"
	"testing"
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
