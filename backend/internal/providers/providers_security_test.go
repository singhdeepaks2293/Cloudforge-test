// Milestone 9, Part 3: provider endpoint SSRF guards — the shared
// security.ValidateEndpointURL rule enforced at construction/probe
// time, not only in the Terraform runner.
package providers

import (
	"context"
	"testing"
)

func TestS3AdapterRefusesNonLoopbackEndpoint(t *testing.T) {
	if _, err := NewS3Adapter("example.com:9000", "test", "test", false); err == nil {
		t.Fatal("public MinIO endpoint must be refused")
	}
	if _, err := NewS3Adapter("169.254.169.254:9000", "test", "test", false); err == nil {
		t.Fatal("cloud metadata endpoint must be refused")
	}
	if _, err := NewS3Adapter("localhost:9000", "test", "test", false); err != nil {
		t.Fatalf("loopback MinIO endpoint must be accepted: %v", err)
	}
	if _, err := NewS3Adapter("minio:9000", "test", "test", false); err != nil {
		t.Fatalf("Docker service name minio must be accepted: %v", err)
	}
}

func TestProbeEmulatorRefusesNonLoopback(t *testing.T) {
	st := ProbeEmulator(context.Background(), "http://example.com:4566")
	if st.Reachable {
		t.Fatal("non-loopback emulator endpoint must not be probed as reachable")
	}
	if st.Detail == "" {
		t.Fatal("refusal must carry a detail")
	}
}

func TestNewAWSRefusesNonLoopbackEndpoints(t *testing.T) {
	if _, err := NewAWS(AWSConfig{DynamoDBEndpoint: "http://10.0.0.5:8000"}, nil); err == nil {
		t.Fatal("non-loopback DynamoDB endpoint must fail NewAWS")
	}
	if _, err := NewAWS(AWSConfig{EmulatorEndpoint: "https://s3.amazonaws.com/"}, nil); err == nil {
		t.Fatal("non-loopback emulator endpoint must fail NewAWS")
	}
	if _, err := NewAWS(AWSConfig{}, nil); err != nil {
		t.Fatalf("default (loopback) endpoints must be accepted: %v", err)
	}
}
