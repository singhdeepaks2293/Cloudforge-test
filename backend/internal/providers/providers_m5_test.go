package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRegistryHealthRefresh(t *testing.T) {
	r := NewRegistry()
	// Probes: s3 ok, dynamodb unavailable, ec2 panics (must be
	// reported unavailable, never crash RefreshHealth).
	r.RegisterProbe("s3", func(ctx context.Context) (string, string) { return HealthOK, "fake ok" })
	r.RegisterProbe("dynamodb", func(ctx context.Context) (string, string) { return HealthUnavailable, "fake down" })
	r.RegisterProbe("ec2", func(ctx context.Context) (string, string) { panic("boom") })
	r.RefreshHealth(context.Background())

	c, ok := r.Get("s3", "CreateBucket")
	if !ok || c.Health != HealthOK || c.CheckedAt.IsZero() {
		t.Fatalf("s3 CreateBucket = %+v, want ok with checked_at", c)
	}
	c, _ = r.Get("dynamodb", "CreateTable")
	if c.Health != HealthUnavailable {
		t.Fatalf("dynamodb CreateTable health = %q, want unavailable", c.Health)
	}
	c, _ = r.Get("ec2", "RunInstances")
	if c.Health != HealthUnavailable {
		t.Fatalf("panicking probe must yield unavailable, got %q", c.Health)
	}
	// A service with no probe stays unknown.
	c, _ = r.Get("lambda", "InvokeFunction")
	if c.Health != HealthUnknown {
		t.Fatalf("lambda health = %q, want unknown (no probe)", c.Health)
	}
	if h, ok := r.ServiceHealth("s3"); !ok || h != HealthOK {
		t.Fatalf("ServiceHealth(s3) = %q %v, want ok true", h, ok)
	}
	if h, _ := r.ServiceHealth("dynamodb"); h != HealthUnavailable {
		t.Fatalf("ServiceHealth(dynamodb) = %q, want unavailable", h)
	}
	if _, ok := r.ServiceHealth("nosuchservice"); ok {
		t.Fatal("unknown service must report ok=false")
	}
}

func TestRegistryCoversRequiredServices(t *testing.T) {
	for _, svc := range []string{"s3", "dynamodb", "iam", "sts", "kms", "sqs", "sns", "eventbridge", "ec2", "lambda", "vpc", "eks"} {
		if caps := NewRegistry().ListByService(svc); len(caps) == 0 {
			t.Errorf("service %s has no capabilities registered", svc)
		}
	}
}

func TestSimulatorsSTSAndKMSRoundTrip(t *testing.T) {
	s := NewSimulators("123456789012", "learner")
	ctx := context.Background()
	creds, err := s.AssumeRole(ctx, "arn:aws:iam::123456789012:role/LabRole", "lab-session")
	if err != nil {
		t.Fatal(err)
	}
	if creds.AccessKeyID == "" || creds.SecretAccessKey == "" || creds.SessionToken == "" {
		t.Fatalf("AssumeRole returned empty credentials: %+v", creds)
	}
	if !creds.Expiration.After(time.Now()) {
		t.Fatalf("credentials must expire in the future: %+v", creds)
	}
	id := s.GetCallerIdentity(ctx)
	if id.Account != "123456789012" || !strings.Contains(id.ARN, "learner") {
		t.Fatalf("GetCallerIdentity = %+v", id)
	}

	key, err := s.CreateKey(ctx, "lab key")
	if err != nil {
		t.Fatal(err)
	}
	ct, err := s.EncryptB64(ctx, key.KeyID, []byte("secret-data"))
	if err != nil {
		t.Fatal(err)
	}
	if ct == "secret-data" || ct == "" {
		t.Fatalf("ciphertext must be non-trivial base64, got %q", ct)
	}
	pt, err := s.DecryptB64(ctx, key.KeyID, ct)
	if err != nil {
		t.Fatal(err)
	}
	if string(pt) != "secret-data" {
		t.Fatalf("decrypt round-trip = %q", pt)
	}
	// Unknown key is an error, never a silent empty result.
	if _, err := s.Decrypt(ctx, "key-nope", []byte("x")); err == nil {
		t.Fatal("decrypt with an unknown key must fail")
	}
}

func TestSimulatorsSQSAndSNSFanout(t *testing.T) {
	s := NewSimulators("", "")
	ctx := context.Background()
	if err := s.CreateQueue(ctx, "orders"); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateTopic(ctx, "events"); err != nil {
		t.Fatal(err)
	}
	if err := s.Subscribe(ctx, "events", "orders"); err != nil {
		t.Fatal(err)
	}
	id, err := s.Publish(ctx, "events", "order-placed")
	if err != nil || id == "" {
		t.Fatalf("Publish = %q %v", id, err)
	}
	msgs, err := s.ReceiveMessages(ctx, "orders", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].Body != "order-placed" {
		t.Fatalf("ReceiveMessages = %+v", msgs)
	}
	// Receive removes: the queue is empty afterwards (documented
	// simulator limitation — no visibility timeout).
	if depth, _ := s.QueueDepth("orders"); depth != 0 {
		t.Fatalf("queue depth after receive = %d, want 0", depth)
	}
	// Direct send/receive.
	if _, err := s.SendMessage(ctx, "orders", "direct"); err != nil {
		t.Fatal(err)
	}
	msgs, err = s.ReceiveMessages(ctx, "orders", 10)
	if err != nil || len(msgs) != 1 || msgs[0].Body != "direct" {
		t.Fatalf("direct send/receive = %+v %v", msgs, err)
	}
	// Unknown queue/topic are errors.
	if _, err := s.SendMessage(ctx, "nope", "x"); err == nil {
		t.Fatal("send to an unknown queue must fail")
	}
	if _, err := s.Publish(ctx, "nope", "x"); err == nil {
		t.Fatal("publish to an unknown topic must fail")
	}
}

func TestSimulatorsEventBridgeMatching(t *testing.T) {
	s := NewSimulators("", "")
	ctx := context.Background()
	if err := s.CreateQueue(ctx, "audit-q"); err != nil {
		t.Fatal(err)
	}
	if err := s.PutRule(ctx, "lab-events", "cloudforge.lab", "LabCreated", "audit-q"); err != nil {
		t.Fatal(err)
	}
	matched, err := s.PutEvents(ctx, []EventEntry{
		{Source: "cloudforge.lab", DetailType: "LabCreated", Detail: `{"lab":"l-1"}`},
		{Source: "cloudforge.lab", DetailType: "LabDestroyed", Detail: `{"lab":"l-1"}`},
		{Source: "other", DetailType: "LabCreated", Detail: `{}`},
	})
	if err != nil {
		t.Fatal(err)
	}
	if matched != 1 {
		t.Fatalf("matched = %d, want 1 (only exact source+detail-type matches)", matched)
	}
	msgs, err := s.ReceiveMessages(ctx, "audit-q", 10)
	if err != nil || len(msgs) != 1 || msgs[0].Body != `{"lab":"l-1"}` {
		t.Fatalf("delivered = %+v %v", msgs, err)
	}
	if len(s.Deliveries()) != 1 || s.Deliveries()[0].RuleName != "lab-events" {
		t.Fatalf("deliveries = %+v", s.Deliveries())
	}
}

func TestEmulatorProbeUnreachable(t *testing.T) {
	st := ProbeEmulator(context.Background(), "http://127.0.0.1:1")
	if st.Reachable {
		t.Fatal("probe of a closed port must report unreachable")
	}
	if st.Detail == "" {
		t.Fatal("probe must explain why the emulator is unreachable")
	}
	if len(st.Services) != 0 {
		t.Fatalf("unreachable probe must report no services: %+v", st)
	}
}

func TestEmulatorProbeLocalStackHealth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_localstack/health" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"services":{"s3":"available","dynamodb":"available","ec2":"disabled"},"version":"4.0.0"}`))
	}))
	defer srv.Close()
	st := ProbeEmulator(context.Background(), srv.URL)
	if !st.Reachable {
		t.Fatalf("probe = %+v, want reachable", st)
	}
	if st.Services["s3"] != "available" || st.Services["ec2"] != "disabled" {
		t.Fatalf("services = %+v", st.Services)
	}
	if st.Version != "4.0.0" {
		t.Fatalf("version = %q", st.Version)
	}
}

func TestAWSWiringRegistersProbes(t *testing.T) {
	a, err := NewAWS(AWSConfig{}, NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	if a.S3 == nil || a.DynamoDB == nil || a.Simulators == nil {
		t.Fatal("NewAWS must build all backends")
	}
	if a.EmulatorEndpoint != DefaultEmulatorEndpoint {
		t.Fatalf("emulator endpoint = %q, want default", a.EmulatorEndpoint)
	}
	// RefreshHealth against dead endpoints: s3/dynamodb/ec2 must end
	// up unavailable, simulators ok — never assumed, always probed.
	a.RefreshHealth(context.Background())
	if h, _ := a.Registry.ServiceHealth("s3"); h != HealthUnavailable {
		t.Fatalf("s3 health with no MinIO = %q, want unavailable", h)
	}
	if h, _ := a.Registry.ServiceHealth("iam"); h != HealthOK {
		t.Fatalf("iam (in-process simulator) health = %q, want ok", h)
	}
	if h, _ := a.Registry.ServiceHealth("ec2"); h != HealthUnavailable {
		t.Fatalf("ec2 health with no emulator = %q, want unavailable", h)
	}
}
