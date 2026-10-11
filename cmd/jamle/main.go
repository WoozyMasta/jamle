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
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

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

// errCLIInputRequired reports that the command did not receive an input path.
var errCLIInputRequired = errors.New("input path is required")

// cliOptions contains values parsed from command-line flags and arguments.
type cliOptions struct {
	// Args contains the optional input and output paths.
	Args struct {
		Input  string // Input is the source file path, or "-" for stdin.
		Output string // Output is the destination file path, or "-" for stdout.
	}

	To                    string   // To selects the output format.
	IgnoreExpandPaths     []string // IgnoreExpandPaths contains repeatable path patterns excluded from expansion.
	Indent                int      // Indent controls output indentation.
	MaxBytes              int64    // MaxBytes limits the input size.
	MaxDepth              int      // MaxDepth limits nested and recursive expansion.
	ExpandKeys            bool     // ExpandKeys enables expansion in mapping keys.
	All                   bool     // All decodes all documents in a YAML stream.
	DisableAssignment     bool     // DisableAssignment suppresses ${VAR:=default} environment updates.
	DisableRequiredErrors bool     // DisableRequiredErrors suppresses ${VAR:?error} failures.
	Version               bool     // Version requests CLI version information.
}

// stringListValue implements flag.Value for repeatable string options.
type stringListValue []string

// String returns the current value in flag-compatible form.
func (v *stringListValue) String() string {
	return strings.Join(*v, ",")
}

// Set appends one occurrence of a repeatable string option.
func (v *stringListValue) Set(value string) error {
	*v = append(*v, value)
	return nil
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
	opts, err := parseCLIOptions(os.Args[1:], io.Discard)
	if errors.Is(err, flag.ErrHelp) {
		printCLIHelp(os.Stdout)
		return
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		if errors.Is(err, errCLIInputRequired) {
			fmt.Fprintln(os.Stderr, "Use '-' for stdin or -h/--help for usage.")
		}
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

	if opts.MaxDepth <= 0 {
		fmt.Fprintln(os.Stderr, "Error: --max-depth must be greater than zero")
		os.Exit(2)
	}

	input, err := readInput(opts.Args.Input, opts.MaxBytes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading input: %v\n", err)
		os.Exit(1)
	}

	if len(input) == 0 {
		fmt.Fprintln(os.Stderr, "Error: input is empty")
		os.Exit(1)
	}

	assignment := jamle.AssignmentSet
	if opts.DisableAssignment {
		assignment = jamle.AssignmentIgnore
	}
	required := jamle.RequiredError
	if opts.DisableRequiredErrors {
		required = jamle.RequiredIgnore
	}

	unmarshalOptions := jamle.UnmarshalOptions{
		MaxDepth:          opts.MaxDepth,
		IgnoreExpandPaths: opts.IgnoreExpandPaths,
		ExpandKeys:        opts.ExpandKeys,
		Assignment:        assignment,
		Required:          required,
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

// parseCLIOptions parses flags before up to two positional arguments.
func parseCLIOptions(args []string, output io.Writer) (cliOptions, error) {
	var opts cliOptions
	var ignorePaths stringListValue

	parser := flag.NewFlagSet("jamle", flag.ContinueOnError)
	parser.SetOutput(output)
	parser.Usage = func() {
		_, _ = fmt.Fprintln(output, "Usage: jamle [flags] input [output]")
		_, _ = fmt.Fprintln(output, "  -h, --help\n\tshow this help message")
		parser.PrintDefaults()
	}

	parser.StringVar(&opts.To, "to", "auto", "output format: auto, json, or yaml")
	parser.Var(&ignorePaths, "ignore-expand-path", "skip expansion for a matching YAML key path; can be repeated")
	parser.IntVar(&opts.Indent, "indent", 2, "output indentation; use 0 for compact output")
	parser.Int64Var(&opts.MaxBytes, "max-bytes", 67108864, "maximum input size in bytes")
	parser.IntVar(&opts.MaxDepth, "max-depth", 32, "maximum variable expansion depth")
	parser.BoolVar(&opts.ExpandKeys, "expand-keys", false, "Expand environment expressions in mapping keys.")
	parser.BoolVar(&opts.All, "all", false, "Decode all input documents in a YAML stream.")
	parser.BoolVar(&opts.DisableAssignment, "disable-assignment", false, "Disable assignment expansion.")
	parser.BoolVar(&opts.DisableRequiredErrors, "disable-required-errors", false, "Disable required-variable errors.")
	parser.BoolVar(&opts.Version, "version", false, "Print version information and exit.")

	if err := parser.Parse(args); err != nil {
		return cliOptions{}, err
	}

	switch opts.To {
	case "auto", "json", "yaml":
	default:
		return cliOptions{}, fmt.Errorf("invalid --to value: %q", opts.To)
	}

	if parser.NArg() > 2 {
		return cliOptions{}, fmt.Errorf("expected at most two positional arguments, got %d", parser.NArg())
	}
	if parser.NArg() == 0 && !opts.Version {
		return cliOptions{}, errCLIInputRequired
	}
	if parser.NArg() > 0 {
		opts.Args.Input = parser.Arg(0)
	}
	if parser.NArg() > 1 {
		opts.Args.Output = parser.Arg(1)
	}
	opts.IgnoreExpandPaths = append([]string(nil), ignorePaths...)

	return opts, nil
}

// printCLIHelp writes the complete command usage to output.
func printCLIHelp(output io.Writer) {
	_, _ = parseCLIOptions([]string{"-h"}, output)
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
