package providers

import (
	"encoding/json"
	"testing"
)

func mustPolicy(t *testing.T, js string) Policy {
	t.Helper()
	var p Policy
	if err := json.Unmarshal([]byte(js), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestEvaluateAllow(t *testing.T) {
	p := mustPolicy(t, `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"s3:GetObject","Resource":"arn:aws:s3:::my-bucket/*"}]}`)
	res := Evaluate([]Policy{p}, EvalRequest{Action: "s3:GetObject", Resource: "arn:aws:s3:::my-bucket/key.txt"})
	if res.Decision != DecisionAllow {
		t.Fatalf("decision = %+v, want Allow", res)
	}
	if res.MatchedStatement != 1 {
		t.Fatalf("matched statement = %d, want 1", res.MatchedStatement)
	}
}

func TestEvaluateWildcardActionAndResource(t *testing.T) {
	p := mustPolicy(t, `{"Statement":[{"Effect":"Allow","Action":"s3:*","Resource":"arn:aws:s3:::*"}]}`)
	for _, tc := range []struct{ action, resource string }{
		{"s3:GetObject", "arn:aws:s3:::b/k"},
		{"S3:PutObject", "arn:aws:s3:::b"}, // action match is case-insensitive
		{"s3:ListBuckets", "arn:aws:s3:::*"},
	} {
		if got := Evaluate([]Policy{p}, EvalRequest{Action: tc.action, Resource: tc.resource}); got.Decision != DecisionAllow {
			t.Errorf("%s %s: %+v, want Allow", tc.action, tc.resource, got)
		}
	}
	// "?" matches exactly one character.
	p2 := mustPolicy(t, `{"Statement":[{"Effect":"Allow","Action":"s3:Get?bject","Resource":"*"}]}`)
	if got := Evaluate([]Policy{p2}, EvalRequest{Action: "s3:GetObject", Resource: "x"}); got.Decision != DecisionAllow {
		t.Fatalf("? wildcard: %+v, want Allow", got)
	}
	if got := Evaluate([]Policy{p2}, EvalRequest{Action: "s3:GetObjects", Resource: "x"}); got.Decision != DecisionDeny {
		t.Fatalf("? must not match two chars: %+v", got)
	}
}

func TestEvaluateExplicitDenyWins(t *testing.T) {
	allow := mustPolicy(t, `{"Statement":[{"Effect":"Allow","Action":"s3:*","Resource":"*"}]}`)
	deny := mustPolicy(t, `{"Statement":[{"Effect":"Deny","Action":"s3:DeleteObject","Resource":"arn:aws:s3:::secret/*"}]}`)
	// Deny listed first and last — either way it wins.
	for _, policies := range [][]Policy{{allow, deny}, {deny, allow}} {
		res := Evaluate(policies, EvalRequest{Action: "s3:DeleteObject", Resource: "arn:aws:s3:::secret/key"})
		if res.Decision != DecisionDeny {
			t.Fatalf("policies %v: %+v, want Deny", policies, res)
		}
		if res.MatchedStatement == 0 {
			t.Fatalf("deny must name the matched statement: %+v", res)
		}
	}
	// The same allow still permits a non-denied action.
	res := Evaluate([]Policy{allow, deny}, EvalRequest{Action: "s3:GetObject", Resource: "arn:aws:s3:::secret/key"})
	if res.Decision != DecisionAllow {
		t.Fatalf("non-denied action: %+v, want Allow", res)
	}
}

func TestEvaluateImplicitDeny(t *testing.T) {
	p := mustPolicy(t, `{"Statement":[{"Effect":"Allow","Action":"s3:GetObject","Resource":"arn:aws:s3:::a/*"}]}`)
	res := Evaluate([]Policy{p}, EvalRequest{Action: "ec2:RunInstances", Resource: "arn:aws:ec2:::i-1"})
	if res.Decision != DecisionDeny || res.MatchedStatement != 0 {
		t.Fatalf("unmatched request: %+v, want implicit Deny with no matched statement", res)
	}
}

func TestEvaluateStringEqualsUsername(t *testing.T) {
	p := mustPolicy(t, `{"Statement":[{"Effect":"Allow","Action":"s3:GetObject","Resource":"*","Condition":{"StringEquals":{"aws:username":"alice"}}}]}`)
	if got := Evaluate([]Policy{p}, EvalRequest{Action: "s3:GetObject", Resource: "x", Username: "alice"}); got.Decision != DecisionAllow {
		t.Fatalf("matching username: %+v, want Allow", got)
	}
	if got := Evaluate([]Policy{p}, EvalRequest{Action: "s3:GetObject", Resource: "x", Username: "bob"}); got.Decision != DecisionDeny {
		t.Fatalf("non-matching username: %+v, want Deny", got)
	}
	// List values in the condition.
	p2 := mustPolicy(t, `{"Statement":[{"Effect":"Allow","Action":"*","Resource":"*","Condition":{"StringEquals":{"aws:username":["alice","bob"]}}}]}`)
	if got := Evaluate([]Policy{p2}, EvalRequest{Action: "s3:GetObject", Resource: "x", Username: "bob"}); got.Decision != DecisionAllow {
		t.Fatalf("list condition: %+v, want Allow", got)
	}
}

func TestEvaluateUnsupportedConditionNeverMatches(t *testing.T) {
	p := mustPolicy(t, `{"Statement":[{"Effect":"Allow","Action":"*","Resource":"*","Condition":{"StringLike":{"aws:username":"a*"}}}]}`)
	if got := Evaluate([]Policy{p}, EvalRequest{Action: "s3:GetObject", Resource: "x", Username: "alice"}); got.Decision != DecisionDeny {
		t.Fatalf("unsupported operator must not match: %+v", got)
	}
	p2 := mustPolicy(t, `{"Statement":[{"Effect":"Allow","Action":"*","Resource":"*","Condition":{"StringEquals":{"aws:PrincipalTag/team":"red"}}}]}`)
	if got := Evaluate([]Policy{p2}, EvalRequest{Action: "s3:GetObject", Resource: "x", Username: "alice"}); got.Decision != DecisionDeny {
		t.Fatalf("unsupported condition key must not match: %+v", got)
	}
}

func TestEvaluateMultiplePolicies(t *testing.T) {
	p1 := mustPolicy(t, `{"Statement":[{"Effect":"Allow","Action":"s3:GetObject","Resource":"*"}]}`)
	p2 := mustPolicy(t, `{"Statement":[{"Effect":"Allow","Action":"dynamodb:GetItem","Resource":"*"}]}`)
	if got := Evaluate([]Policy{p1, p2}, EvalRequest{Action: "dynamodb:GetItem", Resource: "arn:aws:dynamodb:::table/t"}); got.Decision != DecisionAllow || got.MatchedStatement != 2 {
		t.Fatalf("second policy allow: %+v", got)
	}
	// Unknown effect never matches.
	p3 := mustPolicy(t, `{"Statement":[{"Effect":"Maybe","Action":"*","Resource":"*"}]}`)
	if got := Evaluate([]Policy{p3}, EvalRequest{Action: "s3:GetObject", Resource: "x"}); got.Decision != DecisionDeny {
		t.Fatalf("unknown effect: %+v, want Deny", got)
	}
}

func TestStringListUnmarshal(t *testing.T) {
	var p Policy
	if err := json.Unmarshal([]byte(`{"Statement":[{"Effect":"Allow","Action":"s3:GetObject","Resource":["a","b"]}]}`), &p); err != nil {
		t.Fatal(err)
	}
	if len(p.Statement[0].Action) != 1 || len(p.Statement[0].Resource) != 2 {
		t.Fatalf("StringList = %+v", p.Statement[0])
	}
}
