package providers

import (
	"context"
	"fmt"

	"github.com/cloudforge/cloudforge/backend/internal/security"
)

// AWSConfig wires the M5 provider backends. Endpoints come from the
// environment via config (see backend/README.md); an empty field
// means "use the package default".
type AWSConfig struct {
	MinIOEndpoint      string
	MinIOAccessKey     string
	MinIOSecretKey     string
	MinIOUseSSL        bool
	DynamoDBEndpoint   string
	EmulatorEndpoint   string
	SimulatorAccountID string
	SimulatorUsername  string
}

// AWS bundles every M5 provider backend behind one handle: the
// capability registry (with health probes registered), the MinIO S3
// adapter, the DynamoDB adapter, the educational simulators and the
// emulator endpoint. A nil adapter field means that backend was not
// configured; its operations then report 503 with capability info
// instead of failing at startup.
type AWS struct {
	Registry         *Registry
	S3               *S3Adapter
	DynamoDB         *DynamoDBAdapter
	Simulators       *Simulators
	EmulatorEndpoint string
}

// NewAWS builds the provider backends from cfg and registers health
// probes on registry (or a fresh registry when nil). It never dials
// anything: reachability is established by RefreshHealth and by the
// first operation.
//
// The DynamoDB and emulator endpoints are SSRF-guarded here, at
// startup, so a misconfigured environment fails fast instead of
// letting a probe or operation fetch an arbitrary host. (The S3
// endpoint is guarded in NewS3Adapter above.)
func NewAWS(cfg AWSConfig, registry *Registry) (*AWS, error) {
	if registry == nil {
		registry = NewRegistry()
	}
	if cfg.DynamoDBEndpoint != "" {
		if err := security.ValidateEndpointURL(cfg.DynamoDBEndpoint); err != nil {
			return nil, fmt.Errorf("dynamodb endpoint refused (SSRF guard): %w", err)
		}
	}
	if cfg.EmulatorEndpoint != "" {
		if err := security.ValidateEndpointURL(cfg.EmulatorEndpoint); err != nil {
			return nil, fmt.Errorf("emulator endpoint refused (SSRF guard): %w", err)
		}
	}
	s3, err := NewS3Adapter(cfg.MinIOEndpoint, cfg.MinIOAccessKey, cfg.MinIOSecretKey, cfg.MinIOUseSSL)
	if err != nil {
		return nil, err
	}
	a := &AWS{
		Registry:         registry,
		S3:               s3,
		DynamoDB:         NewDynamoDBAdapter(cfg.DynamoDBEndpoint),
		Simulators:       NewSimulators(cfg.SimulatorAccountID, cfg.SimulatorUsername),
		EmulatorEndpoint: cfg.EmulatorEndpoint,
	}
	if a.EmulatorEndpoint == "" {
		a.EmulatorEndpoint = DefaultEmulatorEndpoint
	}
	// Health probes: one per backend family. The simulators are
	// in-process, so their probe is trivially ok (and says so);
	// s3/dynamodb/emulator probe real network endpoints.
	registry.RegisterProbe("s3", a.S3.Health)
	registry.RegisterProbe("dynamodb", a.DynamoDB.Health)
	registry.RegisterProbe("iam", a.Simulators.Health)
	registry.RegisterProbe("sts", a.Simulators.Health)
	registry.RegisterProbe("kms", a.Simulators.Health)
	registry.RegisterProbe("sqs", a.Simulators.Health)
	registry.RegisterProbe("sns", a.Simulators.Health)
	registry.RegisterProbe("eventbridge", a.Simulators.Health)
	registry.RegisterProbe("ec2", EmulatorHealthProbe(a.EmulatorEndpoint))
	return a, nil
}

// RefreshHealth probes every registered backend and updates the
// registry. Call it at startup and on demand (the capabilities API
// does so); it is safe to call concurrently.
func (a *AWS) RefreshHealth(ctx context.Context) {
	a.Registry.RefreshHealth(ctx)
}

// EmulatorStatus probes the configured AWS emulator endpoint now.
func (a *AWS) EmulatorStatus(ctx context.Context) EmulatorStatus {
	return ProbeEmulator(ctx, a.EmulatorEndpoint)
}
