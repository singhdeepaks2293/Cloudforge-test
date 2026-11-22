package providers

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// IAM policy evaluation (Milestone 5) — REAL evaluation of a SUBSET
// of the IAM policy language, run locally. This is not AWS IAM.
//
// Supported:
//   - Effect Allow / Deny (anything else: the statement never matches).
//   - Action and Resource as a string or a list of strings, with
//     wildcard matching (* matches any sequence, ? matches one
//     character). Action matching is case-insensitive (as in AWS IAM);
//     resource matching is case-sensitive (ARNs are case-sensitive).
//   - Condition with the StringEquals operator on aws:username only
//     (value a string or a list of strings).
//   - Default deny; an explicit Deny in any statement of any policy
//     wins over any Allow.
//
// NOT supported (a statement carrying one never matches, and the
// reason says so): other condition operators/keys, Principal,
// NotAction/NotResource, policy variables (${...}), and Version
// handling beyond accepting "2012-10-17" and "2008-10-17".
type Policy struct {
	Version   string      `json:"Version"`
	Statement []Statement `json:"Statement"`
}

type Statement struct {
	Effect    string                    `json:"Effect"`
	Action    StringList                `json:"Action"`
	Resource  StringList                `json:"Resource"`
	Condition map[string]map[string]any `json:"Condition,omitempty"`
	// Unsupported fields, detected (not evaluated) so a document
	// that relies on them is rejected at validation time instead of
	// silently never matching. Principal is allowed only in trust
	// policies (see ValidateTrustPolicy), never in identity policies.
	NotAction   StringList      `json:"NotAction,omitempty"`
	NotResource StringList      `json:"NotResource,omitempty"`
	Principal   json.RawMessage `json:"Principal,omitempty"`
}

// StringList unmarshals a JSON string or a list of strings.
type StringList []string

func (s *StringList) UnmarshalJSON(data []byte) error {
	var one string
	if err := json.Unmarshal(data, &one); err == nil {
		*s = []string{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(data, &many); err != nil {
		return fmt.Errorf("expected a string or list of strings: %w", err)
	}
	*s = many
	return nil
}

// EvalRequest is one authorization question.
type EvalRequest struct {
	Action   string // e.g. "s3:GetObject"
	Resource string // e.g. "arn:aws:s3:::my-bucket/key"
	Username string // value of aws:username for conditions; "" when unknown
}

// EvalResult is the decision for one request.
type EvalResult struct {
	Decision string `json:"decision"` // "Allow" or "Deny"
	Reason   string `json:"reason"`
	// MatchedStatement is the 1-based index (across all policies, in
	// order) of the statement that decided the outcome, 0 when no
	// statement matched (implicit deny).
	MatchedStatement int `json:"matched_statement,omitempty"`
}

const (
	DecisionAllow = "Allow"
	DecisionDeny  = "Deny"
)

// TrailEntry is one step of an evaluation trail: every statement
// that was considered, whether it matched, and why. The decision is
// explained by the trail: the first explicit-Deny match decides a
// deny, the first Allow match decides an allow, and no match at all
// is the implicit (default) deny.
type TrailEntry struct {
	PolicyIndex    int    `json:"policy_index"`
	StatementIndex int    `json:"statement_index"`
	Effect         string `json:"effect"`
	Matched        bool   `json:"matched"`
	Reason         string `json:"reason"`
	Decides        bool   `json:"decides,omitempty"`
}

// EvaluateWithTrail evaluates req and returns the decision plus the
// full trail. It shares the matching logic with Evaluate, so the two
// can never disagree.
func EvaluateWithTrail(policies []Policy, req EvalRequest) (EvalResult, []TrailEntry) {
	trail := []TrailEntry{}
	allowIdx := 0
	stmtNo := 0
	for pi, p := range policies {
		for si, st := range p.Statement {
			stmtNo++
			matched, reason := st.matches(req)
			entry := TrailEntry{
				PolicyIndex: pi, StatementIndex: si,
				Effect: st.Effect, Matched: matched, Reason: reason,
			}
			if !matched {
				entry.Reason = diagnoseMismatch(st, req)
				trail = append(trail, entry)
				continue
			}
			switch strings.ToLower(st.Effect) {
			case "deny":
				entry.Decides = true
				trail = append(trail, entry)
				return EvalResult{Decision: DecisionDeny, Reason: "explicit deny: " + reason, MatchedStatement: stmtNo}, trail
			case "allow":
				if allowIdx == 0 {
					allowIdx = stmtNo
					entry.Decides = true
				}
			}
			trail = append(trail, entry)
		}
	}
	if allowIdx > 0 {
		return EvalResult{Decision: DecisionAllow, Reason: "allowed by matching statement", MatchedStatement: allowIdx}, trail
	}
	return EvalResult{Decision: DecisionDeny, Reason: "implicit deny: no statement matched"}, trail
}

// diagnoseMismatch explains, for a statement that did not match,
// which check failed. It never claims support for something the
// evaluator does not do.
func diagnoseMismatch(st Statement, req EvalRequest) string {
	switch strings.ToLower(st.Effect) {
	case "allow", "deny":
	default:
		return "unsupported effect " + st.Effect + " (only Allow/Deny are evaluated)"
	}
	if len(st.NotAction) > 0 || len(st.NotResource) > 0 {
		return "NotAction/NotResource are not supported by this evaluator"
	}
	if len(st.Principal) > 0 {
		return "Principal is not supported by this evaluator"
	}
	if !anyMatch(st.Action, req.Action, true) {
		return "action " + req.Action + " does not match any Action pattern"
	}
	if !anyMatch(st.Resource, req.Resource, false) {
		return "resource " + req.Resource + " does not match any Resource pattern"
	}
	if ok, why := st.conditionMatches(req); !ok {
		if why != "" {
			return why
		}
		return "condition did not match"
	}
	return "statement did not match"
}

// ValidatePolicyDocument parses and validates an identity-policy
// document and runs it through the evaluator (a probe evaluation),
// so a document is never stored without having been evaluated.
// It rejects: malformed JSON, a missing/unknown Version, no
// statements, an Effect other than Allow/Deny, empty Action or
// Resource, policy variables (${...}, unsupported), NotAction /
// NotResource / Principal, and any condition other than StringEquals
// on aws:username.
func ValidatePolicyDocument(raw json.RawMessage) (Policy, error) {
	var p Policy
	if err := json.Unmarshal(raw, &p); err != nil {
		return Policy{}, fmt.Errorf("policy document is not valid JSON: %w", err)
	}
	if err := validatePolicy(p, false); err != nil {
		return Policy{}, err
	}
	// Probe-evaluate: the document must run through the real
	// evaluator before it is stored. The probe request is fixed
	// (it is only a smoke check that evaluation works); the
	// decision itself is not meaningful for an arbitrary document.
	_ = Evaluate([]Policy{p}, EvalRequest{Action: "iam:GetPolicy", Resource: "*"})
	return p, nil
}

// ValidateTrustPolicy validates a role trust-policy document. Like
// an identity policy, except Principal is allowed (it names who may
// assume the role) and every statement must allow sts:AssumeRole (or
// "*"). Trust is recorded, not evaluated: assuming a role is an
// explicit admin API call in CloudForge, and the trust document is
// shown on the role for the learner to read.
func ValidateTrustPolicy(raw json.RawMessage) (Policy, error) {
	var p Policy
	if err := json.Unmarshal(raw, &p); err != nil {
		return Policy{}, fmt.Errorf("trust policy document is not valid JSON: %w", err)
	}
	if err := validatePolicy(p, true); err != nil {
		return Policy{}, err
	}
	for _, st := range p.Statement {
		ok := false
		for _, a := range st.Action {
			if strings.EqualFold(a, "sts:AssumeRole") || a == "*" || a == "sts:*" {
				ok = true
			}
		}
		if !ok {
			return Policy{}, fmt.Errorf("trust policy statements must allow sts:AssumeRole (found actions %v)", []string(st.Action))
		}
	}
	return p, nil
}

func validatePolicy(p Policy, trust bool) error {
	if p.Version != "2012-10-17" && p.Version != "2008-10-17" {
		return fmt.Errorf("policy Version must be 2012-10-17 or 2008-10-17 (got %q)", p.Version)
	}
	if len(p.Statement) == 0 {
		return fmt.Errorf("policy must contain at least one Statement")
	}
	for i, st := range p.Statement {
		n := i + 1
		switch strings.ToLower(st.Effect) {
		case "allow", "deny":
		default:
			return fmt.Errorf("statement %d: Effect must be Allow or Deny (got %q)", n, st.Effect)
		}
		if len(st.Action) == 0 {
			return fmt.Errorf("statement %d: Action is required", n)
		}
		if len(st.Resource) == 0 {
			return fmt.Errorf("statement %d: Resource is required", n)
		}
		for _, a := range append(append([]string{}, st.Action...), st.Resource...) {
			if strings.Contains(a, "${") {
				return fmt.Errorf("statement %d: policy variables (${...}) are not supported by this evaluator", n)
			}
		}
		if len(st.NotAction) > 0 || len(st.NotResource) > 0 {
			return fmt.Errorf("statement %d: NotAction/NotResource are not supported by this evaluator", n)
		}
		if len(st.Principal) > 0 && !trust {
			return fmt.Errorf("statement %d: Principal is not supported in identity policies (use a trust policy on a role)", n)
		}
		for op, conds := range st.Condition {
			if !strings.EqualFold(op, "StringEquals") {
				return fmt.Errorf("statement %d: condition operator %q is not supported (only StringEquals)", n, op)
			}
			for key := range conds {
				if !strings.EqualFold(key, "aws:username") {
					return fmt.Errorf("statement %d: condition key %q is not supported (only aws:username)", n, key)
				}
			}
		}
	}
	return nil
}

// Evaluate evaluates req against policies in order. Explicit deny
// wins over allow; with no matching statement the result is an
// implicit deny.
func Evaluate(policies []Policy, req EvalRequest) EvalResult {
	allowIdx := 0
	stmtNo := 0
	for _, p := range policies {
		for _, st := range p.Statement {
			stmtNo++
			matched, reason := st.matches(req)
			if !matched {
				continue
			}
			switch strings.ToLower(st.Effect) {
			case "deny":
				return EvalResult{Decision: DecisionDeny, Reason: "explicit deny: " + reason, MatchedStatement: stmtNo}
			case "allow":
				if allowIdx == 0 {
					allowIdx = stmtNo
				}
			}
		}
	}
	if allowIdx > 0 {
		return EvalResult{Decision: DecisionAllow, Reason: "allowed by matching statement", MatchedStatement: allowIdx}
	}
	return EvalResult{Decision: DecisionDeny, Reason: "implicit deny: no statement matched"}
}

// matches reports whether the statement applies to req and why.
func (st Statement) matches(req EvalRequest) (bool, string) {
	switch strings.ToLower(st.Effect) {
	case "allow", "deny":
	default:
		return false, ""
	}
	if !anyMatch(st.Action, req.Action, true) {
		return false, ""
	}
	if !anyMatch(st.Resource, req.Resource, false) {
		return false, ""
	}
	if ok, why := st.conditionMatches(req); !ok {
		return false, why
	}
	return true, fmt.Sprintf("statement effect=%s action=%s resource=%s", st.Effect, req.Action, req.Resource)
}

// conditionMatches evaluates the statement's Condition. Only
// StringEquals on aws:username is supported; any other operator or
// key means the statement does not match.
func (st Statement) conditionMatches(req EvalRequest) (bool, string) {
	if len(st.Condition) == 0 {
		return true, ""
	}
	for op, conds := range st.Condition {
		if !strings.EqualFold(op, "StringEquals") {
			return false, "unsupported condition operator " + op
		}
		for key, want := range conds {
			if !strings.EqualFold(key, "aws:username") {
				return false, "unsupported condition key " + key
			}
			if !stringEqualsValue(want, req.Username) {
				return false, "condition StringEquals aws:username did not match"
			}
		}
	}
	return true, ""
}

func stringEqualsValue(want any, got string) bool {
	switch v := want.(type) {
	case string:
		return v == got
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok && s == got {
				return true
			}
		}
	}
	return false
}

func anyMatch(patterns []string, value string, caseInsensitive bool) bool {
	if len(patterns) == 0 {
		return false
	}
	for _, p := range patterns {
		if wildcardMatch(p, value, caseInsensitive) {
			return true
		}
	}
	return false
}

// wildcardMatch implements IAM-style glob matching: * matches any
// sequence (including "/" and ":"), ? matches exactly one character.
func wildcardMatch(pattern, value string, caseInsensitive bool) bool {
	if caseInsensitive {
		pattern, value = strings.ToLower(pattern), strings.ToLower(value)
	}
	var b strings.Builder
	b.WriteString("^")
	for _, r := range pattern {
		switch r {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return false
	}
	return re.MatchString(value)
}
