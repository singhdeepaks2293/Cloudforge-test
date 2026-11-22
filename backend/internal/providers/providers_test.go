package providers

import "testing"

func TestRegistrySeededCapabilities(t *testing.T) {
	r := NewRegistry()
	cases := []struct {
		service, operation, fidelity string
		supported                    bool
	}{
		{"ec2", "RunInstances", FidelityAPIEmulated, true},
		{"s3", "CreateBucket", FidelityAPIEmulated, true},
		{"vpc", "CreateVpc", FidelitySimulated, true},
		// Phase 3: Lambda now has a real subprocess invoker
		// (PARTIAL — real execution, not AWS Lambda, no Docker
		// invoker; the old SIMULATED/Docker claim is gone).
		{"lambda", "InvokeFunction", FidelityPartial, true},
		{"sqs", "ReceiveMessage", FidelityAPIEmulated, true},
		{"sns", "Publish", FidelityAPIEmulated, true},
		{"eventbridge", "PutEvents", FidelityAPIEmulated, true},
		{"dynamodb", "Query", FidelityAPIEmulated, true},
		{"rds", "CreateDBInstance", FidelityPartial, true},
		{"elasticache", "CreateCacheCluster", FidelityPartial, true},
		{"elb", "CreateLoadBalancer", FidelitySimulated, true},
		{"route53", "CreateHostedZone", FidelitySimulated, true},
		{"apigateway", "CreateRestApi", FidelityPartial, true},
	}
	for _, tc := range cases {
		c, ok := r.Get(tc.service, tc.operation)
		if !ok {
			t.Fatalf("%s %s: not registered", tc.service, tc.operation)
		}
		if c.Fidelity != tc.fidelity {
			t.Errorf("%s %s: fidelity %q, want %q", tc.service, tc.operation, c.Fidelity, tc.fidelity)
		}
		if c.Supported != tc.supported {
			t.Errorf("%s %s: supported=%v, want %v", tc.service, tc.operation, c.Supported, tc.supported)
		}
		if len(c.Limitations) == 0 {
			t.Errorf("%s %s: must document limitations", tc.service, tc.operation)
		}
	}
}

func TestNoCapabilityClaimsReal(t *testing.T) {
	for _, c := range NewRegistry().List() {
		if c.Fidelity == FidelityReal {
			t.Errorf("%s %s claims REAL fidelity — nothing here is AWS", c.Service, c.Operation)
		}
		if !c.Supported && c.Fidelity != FidelityUnsupported {
			t.Errorf("%s %s is unsupported but labelled %s", c.Service, c.Operation, c.Fidelity)
		}
	}
}

func TestUnsupportedOperationIsExplicit(t *testing.T) {
	c, ok := NewRegistry().Get("eks", "CreateCluster")
	if !ok {
		t.Fatal("eks CreateCluster should be registered as explicitly unsupported")
	}
	if c.Supported {
		t.Fatal("eks CreateCluster must not be reported as supported")
	}
	if _, ok := NewRegistry().Get("ec2", "NoSuchOperation"); ok {
		t.Fatal("unregistered operations must not be found")
	}
}
