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
