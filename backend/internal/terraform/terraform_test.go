package terraform

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestParsePlanJSONValid(t *testing.T) {
	doc := `{"format_version":"1.2","terraform_version":"1.9.8",
		"planned_values":{"root_module":{"resources":[
			{"address":"aws_vpc.lab","type":"aws_vpc","name":"lab"},
			{"address":"aws_subnet.public","type":"aws_subnet","name":"public"}]},
		 "child_modules":[{"resources":[{"address":"aws_instance.web","type":"aws_instance","name":"web"}]}]},
		"resource_changes":[
			{"address":"aws_vpc.lab","change":{"actions":["create"]}},
			{"address":"aws_instance.web","change":{"actions":["create"]}}]}`
	plan, stored, err := ParsePlanJSON([]byte(doc))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if plan.FormatVersion != "1.2" || plan.TerraformVersion != "1.9.8" {
		t.Fatalf("versions: %+v", plan)
	}
	if len(plan.Resources) != 3 {
		t.Fatalf("resources: %+v", plan.Resources)
	}
	if plan.ActionCounts["create"] != 2 {
		t.Fatalf("action counts: %+v", plan.ActionCounts)
	}
	if stored["format_version"] != "1.2" {
		t.Fatal("stored document lost format_version")
	}
}

func TestParsePlanJSONRejectsNonPlan(t *testing.T) {
	for _, doc := range []string{
		`{"terraform_version":"1.9.8"}`,
		`{"format_version":"1.2"}`,
		`not json at all`,
		`{"format_version":"1.2","planned_values":[]}`,
	} {
		if _, _, err := ParsePlanJSON([]byte(doc)); err == nil {
			t.Fatalf("document should be rejected: %s", doc)
		}
	}
}

func TestParsePlanJSONEmptyPlan(t *testing.T) {
	plan, _, err := ParsePlanJSON([]byte(`{"format_version":"1.0","planned_values":{"root_module":{}}}`))
	if err != nil {
		t.Fatalf("empty plan must parse: %v", err)
	}
	if len(plan.Resources) != 0 {
		t.Fatalf("empty plan must have no resources: %+v", plan)
	}
}

func TestValidateEndpoint(t *testing.T) {
	for _, good := range []string{
		"http://localhost:4566", "http://localhost:9000",
		"http://127.0.0.1:4566", "http://127.0.0.9:8000", "http://[::1]:4566",
		"https://localhost:4566",
	} {
		if err := ValidateEndpoint(good); err != nil {
			t.Fatalf("endpoint %q should be accepted: %v", good, err)
		}
	}
	for _, bad := range []string{
		"", "http://169.254.169.254/latest/meta-data/",
		"http://10.0.0.5:4566", "http://192.168.1.1:4566",
		"http://ec2.amazonaws.com/", "https://s3.amazonaws.com/",
		"ftp://localhost:21", "http://user:pass@localhost:4566",
		"http://localhost.localdomain:4566", "http://example.com/",
	} {
		if err := ValidateEndpoint(bad); err == nil {
			t.Fatalf("endpoint %q should be rejected (SSRF guard)", bad)
		}
	}
}

func TestValidateProviderConfigRefusesRealCredentials(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIAIOSFODNN7EXAMPLE")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY")
	if err := ValidateProviderConfig("http://localhost:4566", "localhost:9000"); err == nil {
		t.Fatal("real-looking AWS credentials must be refused")
	}
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	if err := ValidateProviderConfig("http://localhost:4566", "localhost:9000"); err != nil {
		t.Fatalf("emulator defaults must be accepted: %v", err)
	}
	if err := ValidateProviderConfig("http://example.com:4566", "localhost:9000"); err == nil {
		t.Fatal("non-loopback emulator endpoint must be refused")
	}
}

func TestValidateConfiguration(t *testing.T) {
	if err := ValidateConfiguration(`resource "aws_vpc" "lab" { cidr_block = "10.0.0.0/16" }`); err != nil {
		t.Fatalf("plain config must pass: %v", err)
	}
	if err := ValidateConfiguration(`# endpoint "https://s3.amazonaws.com"`); err == nil {
		t.Fatal("amazonaws.com reference must be rejected")
	}
	if err := ValidateConfiguration(strings.Repeat("x", 65*1024)); err == nil {
		t.Fatal("oversize configuration must be rejected")
	}
}

func TestWriteProviderConfig(t *testing.T) {
	dir := t.TempDir()
	if err := WriteProviderConfig(dir, "http://localhost:4566", "localhost:9000"); err != nil {
		t.Fatalf("write provider config: %v", err)
	}
	data, err := os.ReadFile(dir + "/provider.tf")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "http://localhost:4566") {
		t.Fatal("provider.tf must point at the emulator endpoint")
	}
	if strings.Contains(string(data), "amazonaws.com") {
		t.Fatal("provider.tf must not reference real AWS")
	}
}

func TestContainerRunnerUnavailableInSandbox(t *testing.T) {
	// This sandbox has no Docker socket; the runner must report
	// Unavailable honestly, never fake success.
	r := NewContainerRunner("", "/nonexistent/docker.sock")
	ok, reason := r.Available(context.Background())
	if ok {
		t.Fatal("runner must not report available without a Docker socket")
	}
	if reason == "" {
		t.Fatal("unavailable reason must explain why")
	}
	if _, err := r.Run(context.Background(), RunRequest{Operation: OpPlan, Workdir: t.TempDir()}); !IsUnavailable(err) {
		t.Fatalf("Run without Docker must return UnavailableError, got %v", err)
	}
	// The default socket is also absent here.
	def := NewContainerRunner("", "")
	if ok, _ := def.Available(context.Background()); ok {
		t.Fatal("default Docker socket must be absent in this sandbox")
	}
}

func TestTestStubRunnerIsLabelled(t *testing.T) {
	s := &TestStubRunner{}
	res, err := s.Run(context.Background(), RunRequest{Operation: OpPlan, Workdir: "/tmp/x"})
	if err != nil {
		t.Fatalf("stub run: %v", err)
	}
	if !res.Stub || res.Plan == nil || len(res.Plan.Resources) != 3 {
		t.Fatalf("stub plan must be labelled and parsed: %+v", res)
	}
	if len(s.Calls) != 1 || s.Calls[0].Operation != OpPlan {
		t.Fatalf("stub must record calls: %+v", s.Calls)
	}
	s.FailOperation = map[Operation]error{OpApply: context.DeadlineExceeded}
	if _, err := s.Run(context.Background(), RunRequest{Operation: OpApply}); err == nil {
		t.Fatal("stub FailOperation must fail the run")
	}
}
