/*
Package jamle (JSON and YAML with Env) provides a unified way to unmarshal
YAML/JSON data with Bash-style environment variable expansion.

It wraps the robust "github.com/invopop/yaml" library, allowing you to use
standard json struct tags for YAML files while adding powerful dynamic configuration capabilities.

It supports recursion (nested variables) and the following variable expansion syntax:

  - ${VAR}           Value of VAR, or empty string if unset.
  - ${VAR:-default}  Value of VAR, or "default" if VAR is unset or empty.
  - ${VAR:default}   Same as above (shorthand).
  - ${VAR:=default}  Value of VAR, or "default" if unset/empty. Also sets VAR to "default" in the current environment.
  - ${VAR:?error}    Value of VAR, or returns an error with "error" message if VAR is unset or empty.
  - $${VAR}          Escaping. Evaluates to the literal string ${VAR} without expansion.

Example usage:

	type Config struct {
	    Host string `json:"host"` // Works for YAML too
	    Port int    `json:"port"`
	}

	data := []byte(`host: ${HOST:localhost}`)
	var cfg Config
	err := jamle.Unmarshal(data, &cfg)
*/
package jamle

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/invopop/yaml"
	yamlv3 "gopkg.in/yaml.v3"
)

// placeholders for masking braces in escaped variables
const (
	maskStart = "\x00"
	maskEnd   = "\x01"
)

// envVarRegex matches the innermost ${...} pattern containing no nested braces.
// This allows for "inside-out" expansion logic to handle nested variables like ${A:=${B}}.
var envVarRegex = regexp.MustCompile(`\$\{([^{}]+)\}`)

// escapedVarRegex matches the $${...} pattern for escaping.
var escapedVarRegex = regexp.MustCompile(`\$\$\{([^{}]+)\}`)

/*
Unmarshal parses the YAML-encoded data and stores the result in the value pointed to by v.

Before parsing, it recursively expands environment variables within the data.
The function performs up to 10 passes to resolve nested variables (e.g., ${A:=${B}})
and prevents infinite loops.
*/
func Unmarshal(data []byte, v any) error {
	// Parse into YAML AST (comments are stored in node fields, not in scalar values)
	var root yamlv3.Node
	dec := yamlv3.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(false)
	if err := dec.Decode(&root); err != nil {
		return err
	}

	// Expand only scalar values (never comments)
	var resolveErr error
	walkScalars(&root, func(s string) string {
		if resolveErr != nil {
			return s
		}

		out, err := expandEnvInScalar(s)
		if err != nil {
			resolveErr = err
			return s
		}

		return out
	})

	if resolveErr != nil {
		return resolveErr
	}

	// Encode back to YAML (comments preserved) then unmarshal via invopop/yaml
	var buf bytes.Buffer
	enc := yamlv3.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&root); err != nil {
		_ = enc.Close()
		return err
	}

	if err := enc.Close(); err != nil {
		return err
	}

	return yaml.Unmarshal(buf.Bytes(), v)
}

// walkScalars walks the YAML AST recursively and applies fn to the Value of each ScalarNode.
// Comments (HeadComment, LineComment, FootComment) are intentionally not touched.
func walkScalars(n *yamlv3.Node, fn func(string) string) {
	if n == nil {
		return
	}

	if n.Kind == yamlv3.ScalarNode {
		oldStyle := n.Style
		oldTag := n.Tag
		oldVal := n.Value

		n.Value = fn(n.Value)

		// If the scalar was plain and implicitly a string only because it contained ${...},
		// clear the tag so YAML can re-resolve types (bool/int/float/null) after expansion.
		if oldStyle == 0 && oldTag == "!!str" && oldVal != n.Value {
			n.Tag = ""
		}
	}

	for _, c := range n.Content {
		walkScalars(c, fn)
	}
}

// expandEnvInScalar expands Bash-style environment variables inside a single YAML scalar value.
// The function operates only on the provided scalar string and has
// no visibility into YAML structure or comments.
func expandEnvInScalar(in string) (string, error) {
	str := escapedVarRegex.ReplaceAllString(in, maskStart+"$1"+maskEnd)
	var resolveErr error

	// Mask escaped variables $${VAR} -> \x00VAR\x01
	str = escapedVarRegex.ReplaceAllString(str, maskStart+"$1"+maskEnd)

	// Main expansion loop
	for i := 0; i < 10; i++ {
		if !envVarRegex.MatchString(str) {
			break
		}

		replacement := envVarRegex.ReplaceAllStringFunc(str, func(match string) string {
			if resolveErr != nil {
				return match
			}

			content := match[2 : len(match)-1]

			val, err := resolveVariable(content)
			if err != nil {
				resolveErr = err
				return match
			}

			return val
		})

		if resolveErr != nil {
			return "", resolveErr
		}

		if str == replacement {
			break
		}

		str = replacement
	}

	// Unmask \x00VAR\x01 -> ${VAR}
	str = strings.ReplaceAll(str, maskStart, "${")
	str = strings.ReplaceAll(str, maskEnd, "}")

	return str, nil
}

// resolveVariable parses the content inside ${...} and applies Bash-style logic.
// It handles default values, assignments, and error enforcement.
func resolveVariable(content string) (string, error) {
	name, val, hasColon := strings.Cut(content, ":")
	envVal, exists := os.LookupEnv(name)

	// Case 1: Simple variable ${VAR}
	if !hasColon {
		if exists {
			return envVal, nil
		}

		return "", nil
	}

	// Case 2: Variable with empty default ${VAR:}
	if val == "" {
		if exists {
			return envVal, nil
		}

		return "", nil
	}

	// Case 3: Variable with operator and value
	var operator byte
	var defaultVal string

	// Check the first character after the colon
	switch val[0] {
	case '-', '=', '?':
		operator = val[0]
		defaultVal = val[1:]

	default: // if no op (e.g. ${PORT:8080}) use as ':-'
		operator = '-'
		defaultVal = val
	}

	switch operator {
	case '-': // ${VAR:-default} or ${VAR:default} -> use default if unset
		if exists && envVal != "" {
			return envVal, nil
		}
		return defaultVal, nil

	case '=': // ${VAR:=default} -> use default if unset and set env
		if exists && envVal != "" {
			return envVal, nil
		}

		if err := os.Setenv(name, defaultVal); err != nil {
			return "", fmt.Errorf("failed to set env var %s: %w", name, err)
		}

		return defaultVal, nil

	case '?': // ${VAR:?message} -> error
		if exists && envVal != "" {
			return envVal, nil
		}

		msg := defaultVal
		if msg == "" {
			msg = "is not set or empty"
		}

		return "", fmt.Errorf("environment variable %q %s", name, msg)
	}

	return "", nil
}
