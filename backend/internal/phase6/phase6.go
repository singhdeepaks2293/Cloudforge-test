// Package phase6 — Advanced services (CloudForge v2, Phase 6).
//
// Pure logic shared by the Phase 6 HTTP handlers (backend/internal/
// httpapi/phase6.go). Nothing here touches the network or a store:
// handlers own persistence, this package owns the semantics that must
// be unit-testable without a database.
//
// The honesty rule applies throughout: every number a handler reports
// comes from a real store, log, or computation in the run. Nothing
// here invents a finding, a metric, a flow, or a model result.
package phase6

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"net"
	"regexp"
	"strings"
)

// --- Athena: read-only SQL guard -------------------------------------------

var athenaForbidden = regexp.MustCompile(`(?i)\b(insert|update|delete|drop|create|alter|truncate|grant|revoke|copy|vacuum|call|do|transaction|commit|rollback|lock|comment|security|set\s+role)\b`)

var athenaIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ValidateAthenaSQL rejects anything that is not a single read-only
// SELECT/WITH statement. Athena in CloudForge is "PostgreSQL SQL, not
// Presto/Athena" — the query runs against the platform Postgres (or an
// RDS instance database), so the guard is what keeps a teaching query
// editor from becoming a write path.
func ValidateAthenaSQL(sql string) error {
	q := strings.TrimSpace(sql)
	if q == "" {
		return errors.New("query is empty")
	}
	if strings.Contains(strings.TrimSuffix(q, ";"), ";") {
		return errors.New("only a single statement is allowed")
	}
	q = strings.TrimSuffix(q, ";")
	lower := strings.ToLower(strings.TrimSpace(q))
	if !strings.HasPrefix(lower, "select") && !strings.HasPrefix(lower, "with") {
		return errors.New("only SELECT (or WITH ... SELECT) statements are allowed")
	}
	if athenaForbidden.MatchString(q) {
		return errors.New("query contains a forbidden keyword (read-only SELECT only)")
	}
	return nil
}

// ValidateIdentifier is the shared Postgres identifier check (table /
// column / database names supplied by callers must never reach SQL
// unquoted).
func ValidateIdentifier(name string) error {
	if !athenaIdentifier.MatchString(name) {
		return fmt.Errorf("invalid identifier %q (letters, digits and _ only, must not start with a digit)", name)
	}
	if len(name) > 63 {
		return fmt.Errorf("identifier %q is too long (max 63 characters)", name)
	}
	return nil
}

// --- SageMaker: real linear regression (ordinary least squares) ------------

// BuiltinDatasets are the tiny datasets a training job can run on
// without any user data. Each row is features...target (last column
// is the target). They are small on purpose: the point is a real
// training run with real metrics, not a big-data claim.
var BuiltinDatasets = map[string][][]float64{
	// size_sqft, rooms -> price_usd (synthetic teaching data).
	"housing": {
		{800, 2, 180000}, {950, 2, 205000}, {1100, 3, 240000},
		{1250, 3, 265000}, {1400, 3, 295000}, {1600, 4, 330000},
		{1800, 4, 365000}, {2000, 4, 400000}, {2200, 5, 440000},
		{2500, 5, 495000},
	},
	// study_hours, sleep_hours -> exam_score.
	"study": {
		{2, 8, 55}, {3, 7, 58}, {4, 8, 66}, {5, 6, 62},
		{6, 7, 74}, {7, 8, 81}, {8, 6, 78}, {9, 7, 88},
		{10, 8, 92}, {5, 9, 70},
	},
}

// LinearModel is a trained model artifact: weights per feature plus
// an intercept, and the metrics the training run actually measured.
type LinearModel struct {
	Weights   []float64 `json:"weights"`
	Intercept float64   `json:"intercept"`
	Features  []string  `json:"features"`
	Target    string    `json:"target"`
	MSE       float64   `json:"mse"`
	RMSE      float64   `json:"rmse"`
	R2        float64   `json:"r2"`
	Rows      int       `json:"rows_trained"`
	Dataset   string    `json:"dataset"`
}

// TrainLinear fits y ~ X by ordinary least squares (normal equations
// solved by Gaussian elimination). It returns an error for degenerate
// data instead of a fabricated accuracy.
func TrainLinear(rows [][]float64, features []string, target, dataset string) (*LinearModel, error) {
	if len(rows) < 3 {
		return nil, errors.New("need at least 3 training rows")
	}
	nFeat := len(rows[0]) - 1
	if nFeat < 1 {
		return nil, errors.New("rows need at least one feature column and one target column")
	}
	for i, r := range rows {
		if len(r) != nFeat+1 {
			return nil, fmt.Errorf("row %d has %d columns, expected %d", i, len(r), nFeat+1)
		}
		for _, v := range r {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return nil, fmt.Errorf("row %d contains a non-finite value", i)
			}
		}
	}
	// Design matrix with a bias column; solve (X'X) b = X'y.
	d := nFeat + 1
	a := make([][]float64, d)
	for i := range a {
		a[i] = make([]float64, d+1)
	}
	for _, r := range rows {
		x := append([]float64{1}, r[:nFeat]...)
		y := r[nFeat]
		for i := 0; i < d; i++ {
			for j := 0; j < d; j++ {
				a[i][j] += x[i] * x[j]
			}
			a[i][d] += x[i] * y
		}
	}
	b, err := solveLinear(a, d)
	if err != nil {
		return nil, fmt.Errorf("training failed (singular design matrix — features are collinear or constant): %w", err)
	}
	m := &LinearModel{Weights: b[1:], Intercept: b[0], Features: features, Target: target, Rows: len(rows), Dataset: dataset}
	var ssRes, ssTot, meanY float64
	for _, r := range rows {
		meanY += r[nFeat]
	}
	meanY /= float64(len(rows))
	for _, r := range rows {
		pred := m.Predict(r[:nFeat])
		ssRes += (r[nFeat] - pred) * (r[nFeat] - pred)
		ssTot += (r[nFeat] - meanY) * (r[nFeat] - meanY)
	}
	m.MSE = ssRes / float64(len(rows))
	m.RMSE = math.Sqrt(m.MSE)
	if ssTot == 0 {
		m.R2 = 0
	} else {
		m.R2 = 1 - ssRes/ssTot
	}
	return m, nil
}

// Predict applies the trained weights. It is the same function the
// inference endpoint uses — the endpoint never invents a prediction.
func (m *LinearModel) Predict(features []float64) float64 {
	out := m.Intercept
	for i, w := range m.Weights {
		if i < len(features) {
			out += w * features[i]
		}
	}
	return out
}

func solveLinear(a [][]float64, n int) ([]float64, error) {
	for col := 0; col < n; col++ {
		pivot := col
		for r := col + 1; r < n; r++ {
			if math.Abs(a[r][col]) > math.Abs(a[pivot][col]) {
				pivot = r
			}
		}
		if math.Abs(a[pivot][col]) < 1e-12 {
			return nil, errors.New("singular matrix")
		}
		a[col], a[pivot] = a[pivot], a[col]
		for r := col + 1; r < n; r++ {
			f := a[r][col] / a[col][col]
			for c := col; c <= n; c++ {
				a[r][c] -= f * a[col][c]
			}
		}
	}
	x := make([]float64, n)
	for i := n - 1; i >= 0; i-- {
		s := a[i][n]
		for j := i + 1; j < n; j++ {
			s -= a[i][j] * x[j]
		}
		x[i] = s / a[i][i]
	}
	return x, nil
}

// --- Bedrock playground (SIMULATED, labelled) ------------------------------

// BedrockTemplateResponse returns a clearly templated response for
// the concept playground. It is NOT a model: it echoes the prompt
// back inside a fixed template so a learner can see the request /
// response shape without any real-model claim and without a paid
// third-party API.
func BedrockTemplateResponse(modelID, prompt string) string {
	trimmed := strings.TrimSpace(prompt)
	if len(trimmed) > 200 {
		trimmed = trimmed[:200] + "…"
	}
	return fmt.Sprintf("[SIMULATED playground response — no model ran. Model %q is a concept entry; CloudForge sends nothing to any AI provider.]\nTemplate summary of your prompt (%d characters): %q\nTo get a real answer, run a SageMaker training job and invoke its endpoint instead.", modelID, len(prompt), trimmed)
}

// --- Kinesis ----------------------------------------------------------------

func KinesisShard(partitionKey string, shardCount int) int {
	if shardCount <= 0 {
		shardCount = 1
	}
	h := sha256.Sum256([]byte(partitionKey))
	return int(h[0]) % shardCount
}

// --- WAF: real rule evaluation ----------------------------------------------

// WAFRule is one stored WAF rule. Match types: path_prefix,
// path_contains, header_equals, query_contains, body_contains,
// ip_prefix. Action: allow or block. First matching rule wins.
type WAFRule struct {
	Name       string `json:"name"`
	Action     string `json:"action"` // allow | block
	MatchType  string `json:"match_type"`
	MatchValue string `json:"match_value"`
	HeaderName string `json:"header_name,omitempty"`
}

// WAFRequest is the sample request a caller asks the rules to judge.
type WAFRequest struct {
	Method   string            `json:"method"`
	Path     string            `json:"path"`
	Query    string            `json:"query"`
	Headers  map[string]string `json:"headers"`
	Body     string            `json:"body"`
	SourceIP string            `json:"source_ip"`
}

// EvaluateWAF runs the rules in order against req. It returns the
// action, the name of the rule that decided it ("" for the default),
// and whether a rule matched.
func EvaluateWAF(rules []WAFRule, req WAFRequest) (action, matchedRule string, matched bool) {
	for _, ru := range rules {
		if wafRuleMatches(ru, req) {
			return ru.Action, ru.Name, true
		}
	}
	return "allow", "", false
}

func wafRuleMatches(ru WAFRule, req WAFRequest) bool {
	switch ru.MatchType {
	case "path_prefix":
		return strings.HasPrefix(req.Path, ru.MatchValue)
	case "path_contains":
		return strings.Contains(req.Path, ru.MatchValue)
	case "query_contains":
		return strings.Contains(req.Query, ru.MatchValue)
	case "body_contains":
		return strings.Contains(req.Body, ru.MatchValue)
	case "header_equals":
		for k, v := range req.Headers {
			if strings.EqualFold(k, ru.HeaderName) && v == ru.MatchValue {
				return true
			}
		}
		return false
	case "ip_prefix":
		_, n, err := net.ParseCIDR(ru.MatchValue)
		if err != nil {
			return req.SourceIP == ru.MatchValue
		}
		ip := net.ParseIP(req.SourceIP)
		return ip != nil && n.Contains(ip)
	}
	return false
}

// --- Network Firewall: real 5-tuple evaluation ------------------------------

// FirewallRule is one stateful network-firewall rule. Port ranges
// are "80" or "8000-8100"; CIDRs are "0.0.0.0/0" or a single IP.
// Protocol is tcp, udp, icmp or any. Lowest priority number wins.
type FirewallRule struct {
	Name       string `json:"name"`
	Priority   int    `json:"priority"`
	Action     string `json:"action"` // allow | deny
	Protocol   string `json:"protocol"`
	SourceCIDR string `json:"source_cidr"`
	DestCIDR   string `json:"dest_cidr"`
	PortRange  string `json:"port_range"`
}

// FirewallPacket is the 5-tuple a caller asks the firewall to judge.
type FirewallPacket struct {
	SourceIP  string `json:"source_ip"`
	DestIP    string `json:"dest_ip"`
	DestPort  int    `json:"dest_port"`
	Protocol  string `json:"protocol"`
	Direction string `json:"direction"` // ingress | egress
}

// EvaluateFirewall returns the action and the deciding rule name
// ("" when the default-deny decided it). Rules are evaluated in
// priority order; anything unmatched is denied.
func EvaluateFirewall(rules []FirewallRule, pkt FirewallPacket) (action, matchedRule string) {
	sorted := append([]FirewallRule(nil), rules...)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].Priority < sorted[i].Priority {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	for _, ru := range sorted {
		if firewallRuleMatches(ru, pkt) {
			return ru.Action, ru.Name
		}
	}
	return "deny", ""
}

func firewallRuleMatches(ru FirewallRule, pkt FirewallPacket) bool {
	if ru.Protocol != "" && ru.Protocol != "any" && !strings.EqualFold(ru.Protocol, pkt.Protocol) {
		return false
	}
	if ru.SourceCIDR != "" && !cidrContains(ru.SourceCIDR, pkt.SourceIP) {
		return false
	}
	if ru.DestCIDR != "" && !cidrContains(ru.DestCIDR, pkt.DestIP) {
		return false
	}
	if ru.PortRange != "" && !portInRange(ru.PortRange, pkt.DestPort) {
		return false
	}
	return true
}

func cidrContains(cidr, ip string) bool {
	if strings.Contains(cidr, "/") {
		_, n, err := net.ParseCIDR(cidr)
		if err != nil {
			return false
		}
		parsed := net.ParseIP(ip)
		return parsed != nil && n.Contains(parsed)
	}
	return cidr == ip
}

func portInRange(spec string, port int) bool {
	if strings.Contains(spec, "-") {
		var lo, hi int
		if _, err := fmt.Sscanf(spec, "%d-%d", &lo, &hi); err != nil {
			return false
		}
		return port >= lo && port <= hi
	}
	var p int
	if _, err := fmt.Sscanf(spec, "%d", &p); err != nil {
		return false
	}
	return port == p
}

// --- Envelope encryption (KMS / Secrets Manager) ----------------------------

// MasterKey derives the 32-byte wrapping key from the operator
// secret. The secret itself is never stored; only its SHA-256 is
// used as an AES-256 key. A distinct CLOUDFORGE_KMS_MASTER_KEY is
// preferred; the JWT secret is the documented fallback.
func MasterKey(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

// WrapKey encrypts a data key under the master key (AES-256-GCM,
// random nonce). The returned string is base64(nonce || ciphertext).
func WrapKey(master, plaintext []byte) (string, error) {
	return aesGCMSeal(master, plaintext)
}

// UnwrapKey reverses WrapKey.
func UnwrapKey(master []byte, wrapped string) ([]byte, error) {
	return aesGCMOpen(master, wrapped)
}

// EncryptValue encrypts a plaintext value under a data key.
func EncryptValue(dataKey []byte, plaintext string) (string, error) {
	return aesGCMSeal(dataKey, []byte(plaintext))
}

// DecryptValue reverses EncryptValue.
func DecryptValue(dataKey []byte, sealed string) (string, error) {
	b, err := aesGCMOpen(dataKey, sealed)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func aesGCMSeal(key, plaintext []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	g, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, g.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := g.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(out), nil
}

func aesGCMOpen(key []byte, sealed string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	g, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(raw) < g.NonceSize() {
		return nil, errors.New("sealed value too short")
	}
	return g.Open(nil, raw[:g.NonceSize()], raw[g.NonceSize():], nil)
}

// --- Cost Explorer: educational cost model ----------------------------------

// CostPricing is the transparent per-unit price list the Cost
// Explorer uses. These are teaching rates for a local lab, not AWS
// prices — the console says so, and every line item names the real
// usage it was computed from.
type CostPricing struct {
	LabHourMicro   float64 `json:"lab_hour_micro"`
	LabHourSmall   float64 `json:"lab_hour_small"`
	LabHourMedium  float64 `json:"lab_hour_medium"`
	StorageGBMonth float64 `json:"storage_gb_month"`
	SQSPer1000     float64 `json:"sqs_per_1000_requests"`
	LambdaPer1000  float64 `json:"lambda_per_1000_invocations"`
	RDSHour        float64 `json:"rds_hour"`
	CacheHour      float64 `json:"cache_hour"`
}

// DefaultPricing is the published teaching price list (USD).
var DefaultPricing = CostPricing{
	LabHourMicro: 0.01, LabHourSmall: 0.02, LabHourMedium: 0.04,
	StorageGBMonth: 0.023, SQSPer1000: 0.0004, LambdaPer1000: 0.0002,
	RDSHour: 0.015, CacheHour: 0.01,
}

// CostLine is one priced usage line: what was used, how much of it,
// and what it costs at the teaching rate.
type CostLine struct {
	Service   string  `json:"service"`
	Resource  string  `json:"resource"`
	Usage     string  `json:"usage"`
	Quantity  float64 `json:"quantity"`
	Unit      string  `json:"unit"`
	UnitPrice float64 `json:"unit_price_usd"`
	CostUSD   float64 `json:"cost_usd"`
}

// ComputeCost prices lab hours, storage, SQS sends, Lambda invokes,
// RDS hours and cache hours from the real counts the caller passes
// in. It never invents usage: a zero count prices at zero.
func ComputeCost(p CostPricing, labHours map[string]float64, storageGB float64, sqsRequests, lambdaInvokes int, rdsHours, cacheHours float64) []CostLine {
	lines := []CostLine{}
	add := func(service, resource, usage string, qty float64, unit string, price float64) {
		if qty == 0 {
			return
		}
		lines = append(lines, CostLine{Service: service, Resource: resource, Usage: usage, Quantity: qty, Unit: unit, UnitPrice: price, CostUSD: qty * price})
	}
	add("EC2", "labs (Micro)", "lab hours", labHours["Micro"], "lab-hour", p.LabHourMicro)
	add("EC2", "labs (Small)", "lab hours", labHours["Small"], "lab-hour", p.LabHourSmall)
	add("EC2", "labs (Medium)", "lab hours", labHours["Medium"], "lab-hour", p.LabHourMedium)
	add("S3", "buckets", "storage", storageGB, "GB-month", p.StorageGBMonth)
	add("SQS", "queues", "requests", float64(sqsRequests)/1000, "1000 requests", p.SQSPer1000)
	add("Lambda", "functions", "invocations", float64(lambdaInvokes)/1000, "1000 invocations", p.LambdaPer1000)
	add("RDS", "instances", "instance hours", rdsHours, "instance-hour", p.RDSHour)
	add("ElastiCache", "clusters", "cluster hours", cacheHours, "cluster-hour", p.CacheHour)
	return lines
}

// CostTotal sums priced lines.
func CostTotal(lines []CostLine) float64 {
	var t float64
	for _, l := range lines {
		t += l.CostUSD
	}
	return t
}
