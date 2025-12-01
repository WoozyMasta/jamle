/*
Command jamle is a command-line utility for processing YAML and JSON files
with Bash-style environment variable expansion.

It reads input from a specified file or from standard input (stdin),
expands variables (e.g., ${VAR}, ${VAR:-default}, ${VAR:?error}), and
outputs the resulting configuration as formatted JSON to standard output (stdout).

Usage:

	jamle [file]

If [file] is omitted or is "-", jamle reads from stdin.

Examples:

	# Read a file and output JSON to stdout
	jamle config.yaml

	# Pipe content into jamle (useful for chaining with jq)
	cat config.yaml | jamle | jq .database.host

	# Verify a config file against current environment variables
	export DB_HOST=prod-db
	jamle production.yaml

This tool is particularly useful for debugging configuration logic,
checking default values, or converting YAML to JSON for other CLI tools.
*/
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/woozymasta/jamle"
)

func main() {
	var input []byte
	var err error

	if len(os.Args) < 2 || os.Args[1] == "-" {
		input, err = io.ReadAll(os.Stdin)
	} else {
		input, err = os.ReadFile(os.Args[1])
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading input: %v\n", err)
		os.Exit(1)
	}

	if len(input) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: jamle <yaml|json-file> OR cat config.yaml | jamle")
		os.Exit(1)
	}

	var config interface{}
	if err := jamle.Unmarshal(input, &config); err != nil {
		fmt.Fprintf(os.Stderr, "Error processing file: %v\n", err)
		os.Exit(1)
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(config); err != nil {
		fmt.Fprintf(os.Stderr, "Error encoding output: %v\n", err)
		os.Exit(1)
	}
}
