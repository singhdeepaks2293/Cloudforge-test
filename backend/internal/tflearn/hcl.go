package tflearn

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// HCLIssue is one validation finding. Severity is "error" or
// "warning". Line is 1-based (0 when the issue has no single line).
type HCLIssue struct {
	Line     int    `json:"line"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

// DeclaredResource is one resource/data block found in configuration.
type DeclaredResource struct {
	Address    string         `json:"address"`
	Type       string         `json:"type"`
	Name       string         `json:"name"`
	Kind       string         `json:"kind"` // "resource" or "data"
	Attributes map[string]any `json:"attributes,omitempty"`
}

// SupportedResourceTypes are the resource types the CloudForge
// Terraform surface accepts in local validation. aws_* types execute
// through the container runner against the local emulator (M7);
// cloudforge_* types are the documented local provider (see
// provider.go) — they validate locally and their execution is the
// CloudForge API mapping, not the AWS provider.
var SupportedResourceTypes = map[string]bool{
	// CloudForge local provider.
	"cloudforge_vpc": true, "cloudforge_subnet": true,
	"cloudforge_security_group": true, "cloudforge_instance": true,
	"cloudforge_s3_bucket": true, "cloudforge_iam_role": true,
	"cloudforge_sqs_queue": true, "cloudforge_dynamodb_table": true,
	"cloudforge_lambda_function": true,
	// AWS provider via the local emulator (M7).
	"aws_vpc": true, "aws_subnet": true, "aws_internet_gateway": true,
	"aws_route_table": true, "aws_route": true, "aws_security_group": true,
	"aws_instance": true, "aws_s3_bucket": true, "aws_iam_role": true,
	"aws_iam_policy": true, "aws_sqs_queue": true, "aws_sns_topic": true,
	"aws_dynamodb_table": true, "aws_lambda_function": true,
	"aws_db_instance": true, "aws_lb": true,
}

var topLevelBlocks = map[string]bool{
	"terraform": true, "provider": true, "resource": true, "data": true,
	"variable": true, "output": true, "module": true, "locals": true,
	"moved": true, "import": true, "check": true, "removed": true,
}

var (
	blockRe  = regexp.MustCompile(`^([a-zA-Z_][a-zA-Z0-9_-]*)\s*(?:"([^"]*)"\s*)?(?:"([^"]*)"\s*)?\{`)
	attrRe   = regexp.MustCompile(`^([a-zA-Z_][a-zA-Z0-9_-]*)\s*=`)
	varRefRe = regexp.MustCompile(`\bvar\.([a-zA-Z_][a-zA-Z0-9_-]*)`)
	resRefRe = regexp.MustCompile(`\b(aws_[a-z_]+|cloudforge_[a-z_]+)\.([a-zA-Z_][a-zA-Z0-9_-]*)\.`)
	secretRe = regexp.MustCompile(`(?i)(aws_access_key_id|aws_secret_access_key|secret_key|password|token)\s*=`)
)

// stripComments removes # and // comments outside strings, keeping
// line numbers intact. It does not try to parse heredocs; heredoc
// bodies are left alone by callers that need them (see FormatHCL).
func stripComments(src string) []string {
	lines := strings.Split(src, "\n")
	for i, line := range lines {
		inStr := false
		for j := 0; j < len(line); j++ {
			c := line[j]
			if c == '"' && (j == 0 || line[j-1] != '\\') {
				inStr = !inStr
			}
			if inStr {
				continue
			}
			if c == '#' || (c == '/' && j+1 < len(line) && line[j+1] == '/') {
				line = line[:j]
				break
			}
		}
		lines[i] = line
	}
	return lines
}

// ValidateHCL checks HCL text without running Terraform. It reports
// real errors (unbalanced delimiters, unknown top-level blocks,
// malformed resource blocks, undeclared variables, references to
// undeclared resources, embedded secrets, amazonaws.com hosts) and
// warnings (unsupported resource types, empty blocks). An empty file
// is valid — a project may legitimately start empty.
func ValidateHCL(src string) []HCLIssue {
	var issues []HCLIssue
	err := func(line int, msg string) {
		issues = append(issues, HCLIssue{Line: line, Severity: "error", Message: msg})
	}
	warn := func(line int, msg string) {
		issues = append(issues, HCLIssue{Line: line, Severity: "warning", Message: msg})
	}
	if strings.Contains(strings.ToLower(src), "amazonaws.com") {
		err(0, "configuration references amazonaws.com; only the local emulator endpoints are allowed")
	}
	if len(src) > 64*1024 {
		err(0, "terraform configuration exceeds the 64 KiB limit")
	}
	lines := stripComments(src)
	// Delimiter balance, tracking strings so braces in strings do
	// not count. Heredoc bodies are skipped: a line starting a
	// heredoc (<<-EOT) suppresses balance checks until the terminator.
	depthBrace, depthParen, depthBrack := 0, 0, 0
	heredocEnd := ""
	declaredVars := map[string]bool{}
	declaredRes := map[string]bool{}
	for i, raw := range lines {
		ln := i + 1
		line := raw
		trimmed := strings.TrimSpace(line)
		if heredocEnd != "" {
			if trimmed == heredocEnd {
				heredocEnd = ""
			}
			continue
		}
		if m := regexp.MustCompile(`<<-?\s*([A-Z_]+)\s*$`).FindStringSubmatch(line); m != nil {
			heredocEnd = m[1]
		}
		if m := secretRe.FindStringSubmatch(line); m != nil {
			err(ln, fmt.Sprintf("configuration must not embed credentials (%s)", m[1]))
		}
		inStr := false
		for j := 0; j < len(line); j++ {
			c := line[j]
			if c == '"' && (j == 0 || line[j-1] != '\\') {
				inStr = !inStr
				continue
			}
			if inStr {
				continue
			}
			switch c {
			case '{':
				depthBrace++
			case '}':
				depthBrace--
				if depthBrace < 0 {
					err(ln, "unexpected closing brace '}' with no matching '{'")
					depthBrace = 0
				}
			case '(':
				depthParen++
			case ')':
				depthParen--
				if depthParen < 0 {
					err(ln, "unexpected closing parenthesis ')' with no matching '('")
					depthParen = 0
				}
			case '[':
				depthBrack++
			case ']':
				depthBrack--
				if depthBrack < 0 {
					err(ln, "unexpected closing bracket ']' with no matching '['")
					depthBrack = 0
				}
			}
		}
		if depthBrace == 1 && strings.HasSuffix(trimmed, "{") {
			m := blockRe.FindStringSubmatch(trimmed)
			if m == nil {
				err(ln, "malformed block header: "+trimmed)
				continue
			}
			kw := m[1]
			if !topLevelBlocks[kw] {
				err(ln, fmt.Sprintf("unknown top-level block %q", kw))
				continue
			}
			switch kw {
			case "resource", "data":
				if m[2] == "" || m[3] == "" {
					err(ln, fmt.Sprintf("%s block needs a quoted type and name: %s \"<type>\" \"<name>\" {", kw, kw))
					continue
				}
				declaredRes[m[2]+"."+m[3]] = true
				if !SupportedResourceTypes[m[2]] {
					warn(ln, fmt.Sprintf("resource type %q is not in the CloudForge provider surface; the container plan will fail on it", m[2]))
				}
			case "variable":
				if m[2] == "" {
					err(ln, "variable block needs a quoted name: variable \"<name>\" {")
					continue
				}
				declaredVars[m[2]] = true
			case "provider":
				if m[2] == "" {
					err(ln, "provider block needs a quoted name: provider \"<name>\" {")
				}
			case "module":
				if m[2] == "" {
					err(ln, "module block needs a quoted name: module \"<name>\" {")
				}
			}
		}
	}
	if depthBrace > 0 {
		err(0, fmt.Sprintf("unbalanced braces: %d unclosed '{'", depthBrace))
	}
	if depthParen > 0 {
		err(0, fmt.Sprintf("unbalanced parentheses: %d unclosed '('", depthParen))
	}
	if depthBrack > 0 {
		err(0, fmt.Sprintf("unbalanced brackets: %d unclosed '['", depthBrack))
	}
	// Second pass: variable and resource references, now that all
	// declarations are known.
	for i, raw := range stripComments(src) {
		ln := i + 1
		for _, m := range varRefRe.FindAllStringSubmatch(raw, -1) {
			if !declaredVars[m[1]] {
				err(ln, fmt.Sprintf("reference to undeclared variable var.%s", m[1]))
			}
		}
		for _, m := range resRefRe.FindAllStringSubmatch(raw, -1) {
			if !declaredRes[m[1]+"."+m[2]] {
				err(ln, fmt.Sprintf("reference to undeclared resource %s.%s", m[1], m[2]))
			}
		}
	}
	sort.Slice(issues, func(i, j int) bool {
		if issues[i].Line != issues[j].Line {
			return issues[i].Line < issues[j].Line
		}
		return issues[i].Severity < issues[j].Severity
	})
	return issues
}

// HasErrors reports whether any issue is an error.
func HasErrors(issues []HCLIssue) bool {
	for _, is := range issues {
		if is.Severity == "error" {
			return true
		}
	}
	return false
}

// FormatHCL re-indents HCL by brace depth (2 spaces per level) and
// normalises spacing around "=" on attribute lines. It is a
// formatter for the editor, not a terraform fmt replacement: it never
// changes tokens, only whitespace, and heredoc bodies are passed
// through untouched. It returns the formatted text.
func FormatHCL(src string) string {
	lines := strings.Split(src, "\n")
	var out []string
	depth := 0
	heredocEnd := ""
	for _, raw := range lines {
		trimmed := strings.TrimSpace(raw)
		if heredocEnd != "" {
			out = append(out, raw)
			if trimmed == heredocEnd {
				heredocEnd = ""
			}
			continue
		}
		if m := regexp.MustCompile(`<<-?\s*([A-Z_]+)\s*$`).FindStringSubmatch(raw); m != nil {
			heredocEnd = m[1]
		}
		if trimmed == "" {
			out = append(out, "")
			continue
		}
		if strings.HasPrefix(trimmed, "}") || strings.HasPrefix(trimmed, ")") || strings.HasPrefix(trimmed, "]") {
			depth--
			if depth < 0 {
				depth = 0
			}
		}
		formatted := trimmed
		if m := attrRe.FindStringSubmatch(trimmed); m != nil && !strings.HasSuffix(trimmed, "{") {
			rest := strings.TrimSpace(trimmed[len(m[0]):])
			formatted = m[1] + " = " + rest
		}
		out = append(out, strings.Repeat("  ", depth)+formatted)
		depth += strings.Count(trimmed, "{") - strings.Count(trimmed, "}")
		depth += strings.Count(trimmed, "(") - strings.Count(trimmed, ")")
		depth += strings.Count(trimmed, "[") - strings.Count(trimmed, "]")
		if depth < 0 {
			depth = 0
		}
	}
	return strings.Join(out, "\n")
}

// ExtractResources returns the resource/data blocks declared in src,
// with their top-level scalar attributes (strings, numbers, booleans)
// captured for plan/drift comparison. Nested blocks are not flattened.
func ExtractResources(src string) []DeclaredResource {
	var out []DeclaredResource
	lines := stripComments(src)
	var cur *DeclaredResource
	depth := 0
	for _, raw := range lines {
		trimmed := strings.TrimSpace(raw)
		if cur == nil {
			if depth == 0 && strings.HasSuffix(trimmed, "{") {
				if m := blockRe.FindStringSubmatch(trimmed); m != nil && (m[1] == "resource" || m[1] == "data") && m[2] != "" && m[3] != "" {
					cur = &DeclaredResource{Address: m[2] + "." + m[3], Type: m[2], Name: m[3], Kind: m[1], Attributes: map[string]any{}}
					depth = 1
					continue
				}
			}
		} else {
			// Inside a resource block: capture depth-1 attributes.
			if depth == 1 {
				if m := attrRe.FindStringSubmatch(trimmed); m != nil {
					val := strings.TrimSpace(trimmed[len(m[0]):])
					val = strings.TrimSuffix(val, ",")
					cur.Attributes[m[1]] = parseScalar(val)
				}
			}
		}
		depth += strings.Count(raw, "{") - strings.Count(raw, "}")
		if cur != nil && depth <= 0 {
			out = append(out, *cur)
			cur = nil
			depth = 0
		}
	}
	if cur != nil {
		out = append(out, *cur)
	}
	if out == nil {
		out = []DeclaredResource{}
	}
	return out
}

func parseScalar(v string) any {
	v = strings.TrimSpace(v)
	if len(v) >= 2 && strings.HasPrefix(v, `"`) && strings.HasSuffix(v, `"`) {
		return v[1 : len(v)-1]
	}
	if v == "true" {
		return true
	}
	if v == "false" {
		return false
	}
	return v
}

// PlanChange is one entry of a local plan preview.
type PlanChange struct {
	Address string `json:"address"`
	Action  string `json:"action"` // create | update | delete | no-op
	Detail  string `json:"detail"`
}

// ComputePlan diffs declared resources against recorded state
// resources. It is a LOCAL static diff (source:
// "cloudforge-local-diff"), not a terraform binary plan — the API
// and UI always label it as such. It never claims resources were
// created.
func ComputePlan(declared, state []DeclaredResource) []PlanChange {
	stateByAddr := map[string]DeclaredResource{}
	for _, r := range state {
		stateByAddr[r.Address] = r
	}
	changes := []PlanChange{}
	for _, d := range declared {
		if s, ok := stateByAddr[d.Address]; ok {
			if attributesEqual(d.Attributes, s.Attributes) {
				changes = append(changes, PlanChange{Address: d.Address, Action: "no-op", Detail: "already matches recorded state"})
			} else {
				changes = append(changes, PlanChange{Address: d.Address, Action: "update", Detail: "attributes differ from recorded state"})
			}
			delete(stateByAddr, d.Address)
		} else {
			changes = append(changes, PlanChange{Address: d.Address, Action: "create", Detail: "not in recorded state"})
		}
	}
	for addr := range stateByAddr {
		changes = append(changes, PlanChange{Address: addr, Action: "delete", Detail: "in recorded state but no longer declared"})
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Address < changes[j].Address })
	return changes
}

func attributesEqual(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || fmt.Sprintf("%v", bv) != fmt.Sprintf("%v", v) {
			return false
		}
	}
	return true
}

// DriftEntry is one drift finding.
type DriftEntry struct {
	Address string `json:"address"`
	Kind    string `json:"kind"` // drifted | unmanaged | missing
	Detail  string `json:"detail"`
}

// DetectDrift compares declared configuration, recorded state and
// live resource addresses. "live" is the set of addresses the
// platform actually has (lab resources, when a lab is linked).
func DetectDrift(declared, state []DeclaredResource, live []string) []DriftEntry {
	stateByAddr := map[string]DeclaredResource{}
	for _, r := range state {
		stateByAddr[r.Address] = r
	}
	liveSet := map[string]bool{}
	for _, a := range live {
		liveSet[a] = true
	}
	var out []DriftEntry
	declaredByAddr := map[string]DeclaredResource{}
	for _, d := range declared {
		declaredByAddr[d.Address] = d
		if s, ok := stateByAddr[d.Address]; ok && !attributesEqual(d.Attributes, s.Attributes) {
			out = append(out, DriftEntry{Address: d.Address, Kind: "drifted", Detail: "declared attributes differ from recorded state"})
		}
		if !liveSet[d.Address] && len(live) > 0 {
			out = append(out, DriftEntry{Address: d.Address, Kind: "missing", Detail: "declared but not present in live resources"})
		}
	}
	for addr := range liveSet {
		if _, ok := declaredByAddr[addr]; !ok {
			out = append(out, DriftEntry{Address: addr, Kind: "unmanaged", Detail: "present live but not declared in configuration"})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Address < out[j].Address })
	if out == nil {
		out = []DriftEntry{}
	}
	return out
}
