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
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/invopop/yaml"
)

// envVarRegex matches the innermost ${...} pattern containing no nested braces.
// This allows for "inside-out" expansion logic to handle nested variables like ${A:=${B}}.
var envVarRegex = regexp.MustCompile(`\$\{([^{}]+)\}`)

/*
Unmarshal parses the YAML-encoded data and stores the result in the value pointed to by v.

Before parsing, it recursively expands environment variables within the data.
The function performs up to 10 passes to resolve nested variables (e.g., ${A:=${B}})
and prevents infinite loops.
*/
func Unmarshal(data []byte, v interface{}) error {
	str := string(data)

	// Loop limit prevents infinite recursion (e.g., A=${A})
	for i := 0; i < 10; i++ {
		// Find the first index of an "innermost" variable
		loc := envVarRegex.FindStringIndex(str)
		if loc == nil {
			break
		}

		// Extract the content inside ${...}
		// loc[0] is start of "${", loc[1] is end of "}"
		content := str[loc[0]+2 : loc[1]-1]

		// Resolve the variable value based on the operator
		val, err := resolveVariable(content)
		if err != nil {
			return err
		}

		// Replace only this specific occurrence.
		// Reconstructing the string ensures indices are valid for the next iteration.
		str = str[:loc[0]] + val + str[loc[1]:]
	}

	return yaml.Unmarshal([]byte(str), v)
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
