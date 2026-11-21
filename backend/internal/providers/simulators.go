package providers

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Simulators implements the educational STS / KMS / SQS / SNS /
// EventBridge simulators (Milestone 5). Everything here is in-memory
// and local to this process: no AWS account, no AWS service, no data
// leaves the deployment. Fidelity is SIMULATED everywhere; the
// registry entries say so. KMS in particular is educational AES-GCM
// with a server-side key — it is NOT real KMS (no HSM, no rotation,
// keys are lost on restart) and must never be presented as such.
type Simulators struct {
	mu sync.Mutex

	// STS: issued credentials, keyed by access key id.
	credentials map[string]stsCredentials
	// KMS: key material, keyed by key id (32 random bytes each).
	kmsKeys map[string][]byte
	// SQS: queues, keyed by queue name.
	queues map[string][]sqsMessage
	// SNS: topics, keyed by topic name.
	topics map[string]*snsTopic
	// EventBridge: rules in insertion order.
	rules []eventRule
	// EventBridge: matched events delivered, newest last.
	deliveries []eventDelivery

	accountID string
	username  string
}

// NewSimulators returns an empty simulator set. accountID/username
// are the local teaching identity reported by GetCallerIdentity.
func NewSimulators(accountID, username string) *Simulators {
	if accountID == "" {
		accountID = "123456789012"
	}
	if username == "" {
		username = "learner"
	}
	return &Simulators{
		credentials: map[string]stsCredentials{},
		kmsKeys:     map[string][]byte{},
		queues:      map[string][]sqsMessage{},
		topics:      map[string]*snsTopic{},
		accountID:   accountID,
		username:    username,
	}
}

// Health reports the simulators as ok — they are in-process and
// available whenever the API is running. The detail says so plainly.
func (s *Simulators) Health(ctx context.Context) (string, string) {
	return HealthOK, "in-memory educational simulators (this process)"
}

// --- STS ----------------------------------------------------------------

type stsCredentials struct {
	AccessKeyID     string    `json:"access_key_id"`
	SecretAccessKey string    `json:"secret_access_key"`
	SessionToken    string    `json:"session_token"`
	Expiration      time.Time `json:"expiration"`
	RoleARN         string    `json:"role_arn"`
}

// AssumeRole returns locally generated credentials. They grant no
// AWS access and are accepted by no AWS service.
func (s *Simulators) AssumeRole(ctx context.Context, roleARN, sessionName string) (stsCredentials, error) {
	if roleARN == "" {
		return stsCredentials{}, errors.New("role ARN is required")
	}
	if sessionName == "" {
		sessionName = "cloudforge-session"
	}
	creds := stsCredentials{
		AccessKeyID:     "ASIA" + randomHex(16),
		SecretAccessKey: randomHex(32),
		SessionToken:    randomHex(64),
		Expiration:      time.Now().UTC().Add(time.Hour),
		RoleARN:         roleARN,
	}
	s.mu.Lock()
	s.credentials[creds.AccessKeyID] = creds
	s.mu.Unlock()
	return creds, nil
}

// CallerIdentity is the local teaching identity.
type CallerIdentity struct {
	Account string `json:"account"`
	ARN     string `json:"arn"`
	UserID  string `json:"user_id"`
}

// GetCallerIdentity returns the local teaching identity — not a real
// AWS identity.
func (s *Simulators) GetCallerIdentity(ctx context.Context) CallerIdentity {
	return CallerIdentity{
		Account: s.accountID,
		ARN:     fmt.Sprintf("arn:aws:iam::%s:user/%s", s.accountID, s.username),
		UserID:  s.username,
	}
}

// --- KMS (educational) --------------------------------------------------

// KMSKey is a simulator key: AES-256 material held in memory.
type KMSKey struct {
	KeyID       string `json:"key_id"`
	Description string `json:"description,omitempty"`
}

// CreateKey generates a new in-memory AES-256 key.
func (s *Simulators) CreateKey(ctx context.Context, description string) (KMSKey, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return KMSKey{}, err
	}
	id := "key-" + randomHex(8)
	s.mu.Lock()
	s.kmsKeys[id] = key
	s.mu.Unlock()
	return KMSKey{KeyID: id, Description: description}, nil
}

// Encrypt encrypts plaintext with AES-256-GCM under the named key.
// The returned bytes are nonce || ciphertext, base64-encoded by the
// caller-facing helpers as needed — this method returns raw bytes.
func (s *Simulators) Encrypt(ctx context.Context, keyID string, plaintext []byte) ([]byte, error) {
	s.mu.Lock()
	key, ok := s.kmsKeys[keyID]
	s.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("kms key %q not found (simulator keys live only in this process)", keyID)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt reverses Encrypt for ciphertext produced by this process.
func (s *Simulators) Decrypt(ctx context.Context, keyID string, ciphertext []byte) ([]byte, error) {
	s.mu.Lock()
	key, ok := s.kmsKeys[keyID]
	s.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("kms key %q not found (simulator keys live only in this process)", keyID)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < gcm.NonceSize() {
		return nil, errors.New("ciphertext too short")
	}
	nonce, ct := ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():]
	return gcm.Open(nil, nonce, ct, nil)
}

// EncryptB64 / DecryptB64 are the base64 forms used over HTTP.
func (s *Simulators) EncryptB64(ctx context.Context, keyID string, plaintext []byte) (string, error) {
	ct, err := s.Encrypt(ctx, keyID, plaintext)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(ct), nil
}

func (s *Simulators) DecryptB64(ctx context.Context, keyID string, ciphertextB64 string) ([]byte, error) {
	ct, err := base64.StdEncoding.DecodeString(ciphertextB64)
	if err != nil {
		return nil, fmt.Errorf("ciphertext is not valid base64: %w", err)
	}
	return s.Decrypt(ctx, keyID, ct)
}

// --- SQS ----------------------------------------------------------------

type sqsMessage struct {
	MessageID string    `json:"message_id"`
	Body      string    `json:"body"`
	SentAt    time.Time `json:"sent_at"`
}

// SQSMessage is a received queue message.
type SQSMessage = sqsMessage

// CreateQueue creates an empty in-memory queue (idempotent).
func (s *Simulators) CreateQueue(ctx context.Context, name string) error {
	if name == "" {
		return errors.New("queue name is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.queues[name]; !ok {
		s.queues[name] = []sqsMessage{}
	}
	return nil
}

// SendMessage appends a message to a queue and returns its id.
func (s *Simulators) SendMessage(ctx context.Context, queue, body string) (string, error) {
	if body == "" {
		return "", errors.New("message body is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.queues[queue]; !ok {
		return "", fmt.Errorf("sqs queue %q does not exist", queue)
	}
	id := "msg-" + randomHex(8)
	s.queues[queue] = append(s.queues[queue], sqsMessage{MessageID: id, Body: body, SentAt: time.Now().UTC()})
	return id, nil
}

// ReceiveMessages removes up to max messages from the queue and
// returns them. Unlike real SQS there is no visibility timeout and
// no redelivery: received messages are gone.
func (s *Simulators) ReceiveMessages(ctx context.Context, queue string, max int) ([]SQSMessage, error) {
	if max < 1 {
		max = 1
	}
	if max > 10 {
		max = 10
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	q, ok := s.queues[queue]
	if !ok {
		return nil, fmt.Errorf("sqs queue %q does not exist", queue)
	}
	if len(q) == 0 {
		return []SQSMessage{}, nil
	}
	if max > len(q) {
		max = len(q)
	}
	out := append([]SQSMessage(nil), q[:max]...)
	s.queues[queue] = append([]sqsMessage(nil), q[max:]...)
	return out, nil
}

// QueueDepth reports the number of pending messages (for tests/health).
func (s *Simulators) QueueDepth(queue string) (int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	q, ok := s.queues[queue]
	return len(q), ok
}

// --- SNS ----------------------------------------------------------------

type snsTopic struct {
	Name        string
	Subscribers []string // SQS queue names
	Published   int
}

// CreateTopic creates an empty in-memory topic (idempotent).
func (s *Simulators) CreateTopic(ctx context.Context, name string) error {
	if name == "" {
		return errors.New("topic name is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.topics[name]; !ok {
		s.topics[name] = &snsTopic{Name: name}
	}
	return nil
}

// Subscribe delivers a topic's messages to an SQS queue.
func (s *Simulators) Subscribe(ctx context.Context, topic, queue string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.topics[topic]
	if !ok {
		return fmt.Errorf("sns topic %q does not exist", topic)
	}
	if _, ok := s.queues[queue]; !ok {
		return fmt.Errorf("sqs queue %q does not exist", queue)
	}
	for _, sub := range t.Subscribers {
		if sub == queue {
			return nil // idempotent
		}
	}
	t.Subscribers = append(t.Subscribers, queue)
	return nil
}

// Publish fans a message out to every subscribed in-memory SQS queue
// and returns the message id.
func (s *Simulators) Publish(ctx context.Context, topic, message string) (string, error) {
	if message == "" {
		return "", errors.New("message is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.topics[topic]
	if !ok {
		return "", fmt.Errorf("sns topic %q does not exist", topic)
	}
	id := "msg-" + randomHex(8)
	for _, queue := range t.Subscribers {
		s.queues[queue] = append(s.queues[queue], sqsMessage{MessageID: id, Body: message, SentAt: time.Now().UTC()})
	}
	t.Published++
	return id, nil
}

// --- EventBridge --------------------------------------------------------

type eventRule struct {
	Name        string
	Source      string // exact match on event source
	DetailType  string // exact match on detail-type
	TargetQueue string
}

type eventDelivery struct {
	RuleName    string    `json:"rule_name"`
	Source      string    `json:"source"`
	DetailType  string    `json:"detail_type"`
	Detail      string    `json:"detail"`
	DeliveredAt time.Time `json:"delivered_at"`
}

// EventEntry is one event submitted to PutEvents.
type EventEntry struct {
	Source     string `json:"source"`
	DetailType string `json:"detail_type"`
	Detail     string `json:"detail"`
}

// PutRule creates a rule matching events by exact source and
// detail-type, delivering matches to an in-memory SQS queue.
func (s *Simulators) PutRule(ctx context.Context, name, source, detailType, targetQueue string) error {
	if name == "" || source == "" || detailType == "" {
		return errors.New("rule name, source and detail-type are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.queues[targetQueue]; !ok {
		return fmt.Errorf("sqs queue %q does not exist", targetQueue)
	}
	for i, r := range s.rules {
		if r.Name == name {
			s.rules[i] = eventRule{Name: name, Source: source, DetailType: detailType, TargetQueue: targetQueue}
			return nil
		}
	}
	s.rules = append(s.rules, eventRule{Name: name, Source: source, DetailType: detailType, TargetQueue: targetQueue})
	return nil
}

// PutEvents matches each event against the rules (exact source and
// detail-type) and delivers matches to the rule's target queue. It
// returns the number of events that matched at least one rule.
func (s *Simulators) PutEvents(ctx context.Context, entries []EventEntry) (matched int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range entries {
		hit := false
		for _, r := range s.rules {
			if r.Source == e.Source && r.DetailType == e.DetailType {
				hit = true
				s.queues[r.TargetQueue] = append(s.queues[r.TargetQueue], sqsMessage{
					MessageID: "evt-" + randomHex(8), Body: e.Detail, SentAt: time.Now().UTC(),
				})
				s.deliveries = append(s.deliveries, eventDelivery{
					RuleName: r.Name, Source: e.Source, DetailType: e.DetailType,
					Detail: e.Detail, DeliveredAt: time.Now().UTC(),
				})
			}
		}
		if hit {
			matched++
		}
	}
	return matched, nil
}

// Deliveries returns the EventBridge deliveries so far (for tests).
func (s *Simulators) Deliveries() []eventDelivery {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]eventDelivery(nil), s.deliveries...)
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// rand.Read from crypto/rand essentially never fails; fall
		// back to a time-based value rather than failing the call.
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
