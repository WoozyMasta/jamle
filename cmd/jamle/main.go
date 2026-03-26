// SPDX-License-Identifier: MIT
// Copyright (c) 2026 WoozyMasta
// Source: github.com/woozymasta/jamle

/*
Command jamle is a command-line utility for processing YAML and JSON files
with Bash-style environment variable expansion.
*/
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jessevdk/go-flags"
	"github.com/woozymasta/jamle"
	"github.com/woozymasta/jamle/yaml"
)

var (
	Version    = "dev"
	Commit     = "unknown"
	BuildTime  = time.Unix(0, 0).UTC()
	URL        = "https://github.com/woozymasta/jamle"
	_buildTime string
)

type cliOptions struct {
	Args struct {
		Input  string `positional-arg-name:"input" description:"Input file path, or '-' for stdin."`
		Output string `positional-arg-name:"output" description:"Output file path, or '-' for stdout."`
	} `positional-args:"yes"`

	To                    string `short:"t" long:"to" choice:"auto" choice:"json" choice:"yaml" default:"auto" description:"Output format. In auto mode, output file extension is used (.json|.yaml|.yml); fallback is json."`
	Indent                int    `short:"i" long:"indent" value-name:"N" default:"2" description:"Output indentation. Use 0 for compact output."`
	MaxBytes              int64  `short:"m" long:"max-bytes" value-name:"N" default:"67108864" description:"Maximum input size in bytes."`
	MaxPasses             int    `short:"p" long:"max-passes" value-name:"N" default:"10" description:"Maximum number of variable expansion passes."`
	All                   bool   `short:"a" long:"all" description:"Decode all input documents (YAML multi-document stream)."`
	DisableAssignment     bool   `short:"A" long:"disable-assignment" description:"Disable side effects of ${VAR:=default}; behaves like ${VAR:-default}."`
	DisableRequiredErrors bool   `short:"R" long:"disable-required-errors" description:"Disable errors for ${VAR:?error}; behaves like ${VAR}."`
	Version               bool   `short:"v" long:"version" description:"Print version information and exit."`
}

func init() {
	if _buildTime == "" {
		return
	}

	parsed, err := time.Parse(time.RFC3339, _buildTime)
	if err != nil {
		return
	}

	BuildTime = parsed.UTC()
}

// main runs the CLI input/read/expand/print flow.
func main() {
	var opts cliOptions
	parser := flags.NewNamedParser("jamle", flags.HelpFlag|flags.PassDoubleDash)
	parser.LongDescription = `jamle reads YAML or JSON and expands environment variables.

Supported placeholders:
* ${VAR}           value of VAR, or empty string if unset.
* ${VAR:-default}  default if VAR is unset or empty.
* ${VAR:=default}  same as above, and sets VAR in current process environment.
* ${VAR:?error}    error if VAR is unset or empty.
* $${VAR}          escaping; keeps literal ${VAR} without expansion.`

	_, err := parser.AddGroup("Options", "", &opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing CLI parser: %v\n", err)
		os.Exit(2)
	}

	_, err = parser.ParseArgs(os.Args[1:])
	if err != nil {
		var flagsErr *flags.Error
		if errors.As(err, &flagsErr) && flagsErr.Type == flags.ErrHelp {
			parser.WriteHelp(os.Stdout)
			return
		}

		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(2)
	}

	if opts.Version {
		printVersionInfo()
		return
	}

	if opts.MaxBytes <= 0 {
		fmt.Fprintln(os.Stderr, "Error: --max-bytes must be greater than zero")
		os.Exit(2)
	}

	if opts.MaxPasses <= 0 {
		fmt.Fprintln(os.Stderr, "Error: --max-passes must be greater than zero")
		os.Exit(2)
	}

	inputPath := opts.Args.Input
	if inputPath == "" {
		inputPath = "-"
	}

	input, err := readInput(inputPath, opts.MaxBytes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading input: %v\n", err)
		os.Exit(1)
	}

	if len(input) == 0 {
		parser.WriteHelp(os.Stderr)
		os.Exit(1)
	}

	unmarshalOptions := jamle.UnmarshalOptions{
		MaxPasses:             opts.MaxPasses,
		DisableAssignment:     opts.DisableAssignment,
		DisableRequiredErrors: opts.DisableRequiredErrors,
	}

	decoded, err := decodeInput(input, opts.All, unmarshalOptions)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error processing file: %v\n", err)
		os.Exit(1)
	}

	outputPath := opts.Args.Output
	if outputPath == "" {
		outputPath = "-"
	}

	outputFormat, err := resolveOutputFormat(opts.To, outputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error encoding output: %v\n", err)
		os.Exit(1)
	}

	output, err := yaml.MarshalWith(decoded, yaml.WriteOptions{
		Format: outputFormat,
		Indent: opts.Indent,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error encoding output: %v\n", err)
		os.Exit(1)
	}

	if err := writeOutput(outputPath, output); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing output: %v\n", err)
		os.Exit(1)
	}
}

// readInput reads input from path or stdin.
func readInput(path string, maxBytes int64) ([]byte, error) {
	var reader io.Reader
	if path == "-" || path == "" {
		reader = os.Stdin
	} else {
		filePath := filepath.Clean(path)
		// #nosec G304,G703 -- CLI intentionally reads a user-provided local file path.
		file, err := os.Open(filePath)
		if err != nil {
			return nil, err
		}
		defer func() {
			_ = file.Close()
		}()
		reader = file
	}

	limited := io.LimitReader(reader, maxBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}

	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("input exceeds --max-bytes (%d bytes)", maxBytes)
	}

	return data, nil
}

// writeOutput writes output to given path or stdout.
func writeOutput(path string, data []byte) error {
	if path == "-" || path == "" {
		_, err := os.Stdout.Write(data)
		return err
	}

	filePath := filepath.Clean(path)
	// #nosec G304 -- CLI intentionally writes to a user-provided local file path.
	return os.WriteFile(filePath, data, 0o600)
}

// decodeInput decodes input into a Go object.
func decodeInput(input []byte, all bool, unmarshalOptions jamle.UnmarshalOptions) (any, error) {
	if all {
		var docs []any
		if err := jamle.UnmarshalAllWithOptions(input, &docs, unmarshalOptions); err != nil {
			return nil, err
		}

		return docs, nil
	}

	var out any
	if err := jamle.UnmarshalWithOptions(input, &out, unmarshalOptions); err != nil {
		return nil, err
	}

	return out, nil
}

// resolveOutputFormat resolves output format from --to and output path.
func resolveOutputFormat(to, outputPath string) (yaml.Format, error) {
	switch strings.ToLower(to) {
	case "json":
		return yaml.FormatJSON, nil
	case "yaml":
		return yaml.FormatYAML, nil
	case "auto":
		ext := strings.ToLower(filepath.Ext(outputPath))
		switch ext {
		case ".yaml", ".yml":
			return yaml.FormatYAML, nil
		case ".json":
			return yaml.FormatJSON, nil
		default:
			return yaml.FormatJSON, nil
		}
	default:
		return "", fmt.Errorf("invalid --to value: %q", to)
	}
}

// printVersionInfo prints CLI build metadata.
func printVersionInfo() {
	fmt.Printf(`url:      %s
file:     %s
version:  %s
commit:   %s
built:    %s
`, URL, os.Args[0], Version, Commit, BuildTime.Format(time.RFC3339))
}
