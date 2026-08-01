package configyaml

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Load parses the YAML file at path, requires owner-only (0600) permissions,
// and returns the flattened env mapping with every ${VAR} reference resolved.
// Scalar values may be scalars of any YAML type; only nested mappings are allowed
// under a key (lists and aliases are rejected).
func Load(path string) (map[string]string, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%s: permissions %#o: want 0600 (owner-only)", path, fi.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("%s: yaml: %w", path, err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 {
		return nil, fmt.Errorf("%s: empty document", path)
	}
	out := make(map[string]string)
	if err := flatten(doc.Content[0], "", out); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return out, nil
}

// Expand resolves every ${VAR} and ${VAR:-default} in s from the environment.
// An unset or empty VAR without a default is an error naming the variable;
// the default is itself expanded (so ${A:-${B}} works) and nested expansion is depth-limited.
func Expand(s string) (string, error) {
	return expand(s, 0)
}

func expand(s string, depth int) (string, error) {
	if depth > 16 {
		return "", fmt.Errorf("too many nested ${...} defaults")
	}
	var out strings.Builder
	rest := s
	for {
		start := strings.Index(rest, "${")
		if start < 0 {
			out.WriteString(rest)
			return out.String(), nil
		}
		out.WriteString(rest[:start])
		// Balanced scan: nested ${...} inside a default must not end the outer ref.
		braces := 0
		closeAt := -1
		for j := start + 2; j < len(rest); j++ {
			switch rest[j] {
			case '{':
				braces++
			case '}':
				if braces == 0 {
					closeAt = j
				} else {
					braces--
				}
			}
			if closeAt >= 0 {
				break
			}
		}
		if closeAt < 0 {
			return "", fmt.Errorf("malformed ${...}: missing closing brace")
		}
		ref := rest[start+2 : closeAt]
		name, def, hasDef := strings.Cut(ref, ":-")
		if !validEnvName(name) {
			return "", fmt.Errorf("malformed ${...}: invalid variable name %q", ref)
		}
		val, ok := os.LookupEnv(name)
		if !ok || val == "" {
			if hasDef {
				d, err := expand(def, depth+1)
				if err != nil {
					return "", fmt.Errorf("%s: default: %w", name, err)
				}
				out.WriteString(d)
			} else {
				return "", fmt.Errorf("%s: environment variable not set", name)
			}
		} else {
			out.WriteString(val)
		}
		rest = rest[closeAt+1:]
	}
}

// flatten walks one mapping node, storing resolved leaf scalars under UPPER_SNAKE
// env names (futu.login_account -> FUTU_LOGIN_ACCOUNT); prefix is the dotted key path.
func flatten(n *yaml.Node, prefix string, out map[string]string) error {
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: %s: want a mapping at the top level", n.Line, prefix)
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if k.Kind != yaml.ScalarNode || !validKey(k.Value) {
			return fmt.Errorf("line %d: %s: invalid key %q (letters, digits, _ and - only; first char a letter or _)", k.Line, prefix, k.Value)
		}
		name := prefix
		if name != "" {
			name += "."
		}
		name += k.Value
		switch v.Kind {
		case yaml.ScalarNode:
			val, err := expand(v.Value, 0)
			if err != nil {
				return fmt.Errorf("line %d: %s: %w", v.Line, name, err)
			}
			out[toEnvName(name)] = val
		case yaml.MappingNode:
			if err := flatten(v, name, out); err != nil {
				return err
			}
		default:
			return fmt.Errorf("line %d: %s: value must be a scalar or nested mapping", v.Line, name)
		}
	}
	return nil
}

func validEnvName(s string) bool {
	if s == "" || !(isLetter(rune(s[0])) || s[0] == '_') {
		return false
	}
	for _, r := range s[1:] {
		if !isLetter(r) && !isDigit(r) && r != '_' {
			return false
		}
	}
	return true
}

// validKey matches one YAML key segment: letters, digits, _ and - (no dots: the
// dotted path is the flattening separator), starting with a letter or _.
func validKey(s string) bool {
	if s == "" || !(isLetter(rune(s[0])) || s[0] == '_') {
		return false
	}
	for _, r := range s {
		if !isLetter(r) && !isDigit(r) && r != '_' && r != '-' {
			return false
		}
	}
	return true
}

func isLetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func isDigit(r rune) bool {
	return r >= '0' && r <= '9'
}

// toEnvName converts a dotted key path to UPPER_SNAKE (futu.login_account -> FUTU_LOGIN_ACCOUNT).
func toEnvName(path string) string {
	var parts []string
	for _, seg := range strings.FieldsFunc(path, func(r rune) bool { return r == '.' || r == '-' }) {
		parts = append(parts, strings.ToUpper(seg))
	}
	return strings.Join(parts, "_")
}
