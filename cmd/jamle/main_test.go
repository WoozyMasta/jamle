package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jessevdk/go-flags"
	"github.com/woozymasta/jamle"
	"github.com/woozymasta/jamle/yaml"
)

func TestCLIOptions_InvalidChoice(t *testing.T) {
	var opts cliOptions
	parser := flags.NewParser(&opts, flags.None)

	_, err := parser.ParseArgs([]string{"--to", "xml"})
	if err == nil {
		t.Fatal("expected ParseArgs to fail for invalid --to choice")
	}
}

func TestCLIOptions_CoreFlags(t *testing.T) {
	args := []string{
		"--to", "yaml",
		"--indent", "4",
		"--max-bytes", "1024",
		"--max-passes", "15",
		"--all",
		"--disable-assignment",
		"--disable-required-errors",
		"in.yaml",
		"out.yaml",
	}

	var opts cliOptions
	parser := flags.NewParser(&opts, flags.None)
	if _, err := parser.ParseArgs(args); err != nil {
		t.Fatalf("ParseArgs returned error: %v", err)
	}

	if opts.To != "yaml" || opts.Indent != 4 || opts.MaxBytes != 1024 || opts.MaxPasses != 15 {
		t.Fatalf("unexpected parsed scalar flags: %#v", opts)
	}
	if !opts.All || !opts.DisableAssignment || !opts.DisableRequiredErrors {
		t.Fatalf("unexpected parsed bool flags: %#v", opts)
	}
	if opts.Args.Input != "in.yaml" || opts.Args.Output != "out.yaml" {
		t.Fatalf("unexpected positional args: input=%q output=%q", opts.Args.Input, opts.Args.Output)
	}
}

func TestCLIOptions_IgnoreExpandPath(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{name: "no flag", args: []string{}, want: nil},
		{name: "single flag", args: []string{"-I", "spec.hooks.*.*.script"}, want: []string{"spec.hooks.*.*.script"}},
		{
			name: "repeatable flag",
			args: []string{
				"--ignore-expand-path", "spec.hooks.*.*.script",
				"--ignore-expand-path", "spec.templates.*.raw",
			},
			want: []string{"spec.hooks.*.*.script", "spec.templates.*.raw"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var opts cliOptions
			parser := flags.NewParser(&opts, flags.None)
			if _, err := parser.ParseArgs(tt.args); err != nil {
				t.Fatalf("ParseArgs returned error: %v", err)
			}

			if !reflect.DeepEqual(opts.IgnoreExpandPaths, tt.want) {
				t.Fatalf("ignore path flags mismatch: got %#v, want %#v", opts.IgnoreExpandPaths, tt.want)
			}
		})
	}
}

func TestReadInput(t *testing.T) {
	t.Run("from file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "in.yaml")
		want := []byte("a: 1\n")
		if err := os.WriteFile(path, want, 0o600); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}

		got, err := readInput(path, int64(len(want)))
		if err != nil {
			t.Fatalf("readInput returned error: %v", err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("readInput mismatch: got %q, want %q", got, want)
		}
	})

	t.Run("stdin path", func(t *testing.T) {
		tmp, err := os.CreateTemp(t.TempDir(), "stdin-*")
		if err != nil {
			t.Fatalf("CreateTemp failed: %v", err)
		}
		defer func() {
			_ = tmp.Close()
		}()

		want := []byte("a: ${A:-b}\n")
		if _, err := tmp.Write(want); err != nil {
			t.Fatalf("temp write failed: %v", err)
		}
		if _, err := tmp.Seek(0, 0); err != nil {
			t.Fatalf("temp seek failed: %v", err)
		}

		old := os.Stdin
		os.Stdin = tmp
		defer func() {
			os.Stdin = old
		}()

		got, err := readInput("-", int64(len(want)))
		if err != nil {
			t.Fatalf("readInput from stdin returned error: %v", err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("stdin read mismatch: got %q, want %q", got, want)
		}
	})

	t.Run("limit exceeded", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "in.yaml")
		if err := os.WriteFile(path, []byte("abcd"), 0o600); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}

		_, err := readInput(path, 3)
		if err == nil || !strings.Contains(err.Error(), "input exceeds --max-bytes") {
			t.Fatalf("expected max-bytes error, got %v", err)
		}
	})

	t.Run("missing file returns error", func(t *testing.T) {
		_, err := readInput(filepath.Join(t.TempDir(), "missing.yaml"), 64)
		if err == nil {
			t.Fatal("expected readInput to fail for missing file")
		}
	})
}

func TestDecodeInput_OptionsAndModes(t *testing.T) {
	t.Run("single and all modes", func(t *testing.T) {
		input := []byte(`
---
a: ${A:-one}
---
a: ${A:-two}
`)

		single, err := decodeInput(input, false, jamle.UnmarshalOptions{})
		if err != nil {
			t.Fatalf("single decodeInput returned error: %v", err)
		}
		singleMap, ok := single.(map[string]any)
		if !ok || singleMap["a"] != "one" {
			t.Fatalf("single output mismatch: %T %#v", single, single)
		}

		all, err := decodeInput(input, true, jamle.UnmarshalOptions{})
		if err != nil {
			t.Fatalf("all decodeInput returned error: %v", err)
		}
		allDocs, ok := all.([]any)
		if !ok || len(allDocs) != 2 {
			t.Fatalf("all output mismatch: %T %#v", all, all)
		}
	})

	t.Run("disable required errors", func(t *testing.T) {
		got, err := decodeInput([]byte("a: ${REQ_MISSING:?boom}\n"), false, jamle.UnmarshalOptions{
			DisableRequiredErrors: true,
		})
		if err != nil {
			t.Fatalf("decodeInput returned error: %v", err)
		}

		root, ok := got.(map[string]any)
		if !ok {
			t.Fatalf("unexpected decode result type: %T", got)
		}

		v, exists := root["a"]
		if !exists || (v != nil && v != "") {
			t.Fatalf("unexpected decode result: %T %#v", got, got)
		}
	})

	t.Run("required errors enabled", func(t *testing.T) {
		_, err := decodeInput(
			[]byte("a: ${REQ_MISSING_2:?boom}\n"),
			false,
			jamle.UnmarshalOptions{},
		)
		if err == nil || !strings.Contains(err.Error(), "boom") {
			t.Fatalf("expected required error with message, got %v", err)
		}
	})

	t.Run("disable assignment", func(t *testing.T) {
		const key = "JAMLE_CLI_DISABLE_ASSIGN"
		_ = os.Unsetenv(key)
		t.Cleanup(func() { _ = os.Unsetenv(key) })

		got, err := decodeInput([]byte("a: ${JAMLE_CLI_DISABLE_ASSIGN:=x}\n"), false, jamle.UnmarshalOptions{
			DisableAssignment: true,
		})
		if err != nil {
			t.Fatalf("decodeInput returned error: %v", err)
		}

		root, ok := got.(map[string]any)
		if !ok || root["a"] != "x" {
			t.Fatalf("unexpected decode result: %T %#v", got, got)
		}
		if _, ok := os.LookupEnv(key); ok {
			t.Fatalf("expected %s to remain unset", key)
		}
	})

	t.Run("assignment enabled sets env", func(t *testing.T) {
		const key = "JAMLE_CLI_ASSIGN_ENABLED"
		_ = os.Unsetenv(key)
		t.Cleanup(func() { _ = os.Unsetenv(key) })

		got, err := decodeInput(
			[]byte("a: ${JAMLE_CLI_ASSIGN_ENABLED:=x}\n"),
			false,
			jamle.UnmarshalOptions{},
		)
		if err != nil {
			t.Fatalf("decodeInput returned error: %v", err)
		}

		root, ok := got.(map[string]any)
		if !ok || root["a"] != "x" {
			t.Fatalf("unexpected decode result: %T %#v", got, got)
		}

		val, ok := os.LookupEnv(key)
		if !ok || val != "x" {
			t.Fatalf("expected %s to be set to x, got %q (ok=%v)", key, val, ok)
		}
	})

	t.Run("escaped placeholders stay literal", func(t *testing.T) {
		got, err := decodeInput([]byte("a: $${LITERAL_VAR}\n"), false, jamle.UnmarshalOptions{})
		if err != nil {
			t.Fatalf("decodeInput returned error: %v", err)
		}

		root, ok := got.(map[string]any)
		if !ok || root["a"] != "${LITERAL_VAR}" {
			t.Fatalf("unexpected decode result: %T %#v", got, got)
		}
	})

	t.Run("invalid yaml returns error", func(t *testing.T) {
		_, err := decodeInput([]byte("{a: 1"), false, jamle.UnmarshalOptions{})
		if err == nil {
			t.Fatal("expected decodeInput to fail on invalid yaml")
		}
	})
}

func TestDecodeInput_IgnoreExpandPathApplied(t *testing.T) {
	input := []byte(`
spec:
  hooks:
    - - script: "echo ${HOOK_SCRIPT:-raw}"
        args: "${HOOK_ARG:-default-arg}"
  token: "${TOKEN:-token-v}"
`)

	decoded, err := decodeInput(input, false, jamle.UnmarshalOptions{
		IgnoreExpandPaths: []string{"spec.hooks.*.*.script"},
	})
	if err != nil {
		t.Fatalf("decodeInput returned error: %v", err)
	}

	spec, step := extractSpecAndFirstHookStep(t, decoded)

	script, _ := step["script"].(string)
	args, _ := step["args"].(string)
	token, _ := spec["token"].(string)

	if script != "echo ${HOOK_SCRIPT:-raw}" {
		t.Fatalf("script must stay unexpanded, got %q", script)
	}
	if args != "default-arg" {
		t.Fatalf("args must be expanded, got %q", args)
	}
	if token != "token-v" {
		t.Fatalf("token must be expanded, got %q", token)
	}
}

func TestDecodeInput_IgnoreExpandPathApplied_AllDocuments(t *testing.T) {
	input := []byte(`
---
spec:
  hooks:
    - - script: "echo ${FIRST_SCRIPT:-raw-1}"
        args: "${FIRST_ARG:-arg-1}"
---
spec:
  hooks:
    - - script: "echo ${SECOND_SCRIPT:-raw-2}"
        args: "${SECOND_ARG:-arg-2}"
`)

	decoded, err := decodeInput(input, true, jamle.UnmarshalOptions{
		IgnoreExpandPaths: []string{"spec.hooks.*.*.script"},
	})
	if err != nil {
		t.Fatalf("decodeInput returned error: %v", err)
	}

	docs, ok := decoded.([]any)
	if !ok || len(docs) != 2 {
		t.Fatalf("unexpected decoded docs: %T %#v", decoded, decoded)
	}

	checkDoc := func(t *testing.T, doc any, expectedScript, expectedArgs string) {
		t.Helper()

		_, step := extractSpecAndFirstHookStep(t, doc)
		script, _ := step["script"].(string)
		args, _ := step["args"].(string)

		if script != expectedScript {
			t.Fatalf("script must stay unexpanded, got %q", script)
		}
		if args != expectedArgs {
			t.Fatalf("args must be expanded, got %q", args)
		}
	}

	checkDoc(t, docs[0], "echo ${FIRST_SCRIPT:-raw-1}", "arg-1")
	checkDoc(t, docs[1], "echo ${SECOND_SCRIPT:-raw-2}", "arg-2")
}

func TestResolveOutputFormat(t *testing.T) {
	tests := []struct {
		name       string
		to         string
		outputPath string
		want       yaml.Format
		wantErr    string
	}{
		{name: "explicit json", to: "json", outputPath: "out.yaml", want: yaml.FormatJSON},
		{name: "explicit yaml", to: "yaml", outputPath: "out.json", want: yaml.FormatYAML},
		{name: "auto by yaml ext", to: "auto", outputPath: "out.yml", want: yaml.FormatYAML},
		{name: "auto by json ext", to: "auto", outputPath: "out.json", want: yaml.FormatJSON},
		{name: "auto unknown ext defaults json", to: "auto", outputPath: "out.bin", want: yaml.FormatJSON},
		{name: "invalid format", to: "xml", outputPath: "out", wantErr: "invalid --to value"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveOutputFormat(tt.to, tt.outputPath)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error %q, got %v", tt.wantErr, err)
				}

				return
			}

			if err != nil {
				t.Fatalf("resolveOutputFormat returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("output format mismatch: got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWriteOutput(t *testing.T) {
	t.Run("to file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "out.json")
		want := []byte(`{"a":1}` + "\n")
		if err := writeOutput(path, want); err != nil {
			t.Fatalf("writeOutput returned error: %v", err)
		}

		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile failed: %v", err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("file output mismatch: got %q, want %q", got, want)
		}
	})

	t.Run("to stdout", func(t *testing.T) {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("Pipe failed: %v", err)
		}
		defer func() {
			_ = r.Close()
		}()

		old := os.Stdout
		os.Stdout = w
		defer func() {
			os.Stdout = old
		}()

		want := []byte("ok\n")
		if err := writeOutput("-", want); err != nil {
			t.Fatalf("writeOutput returned error: %v", err)
		}
		_ = w.Close()

		got, err := io.ReadAll(r)
		if err != nil {
			t.Fatalf("ReadAll failed: %v", err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("stdout output mismatch: got %q, want %q", got, want)
		}
	})

	t.Run("invalid path returns error", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "missing", "out.yaml")
		err := writeOutput(path, []byte("a: 1\n"))
		if err == nil {
			t.Fatal("expected writeOutput to fail for invalid path")
		}
	})
}

// extractSpecAndFirstHookStep returns the decoded spec and first hook step map.
func extractSpecAndFirstHookStep(t *testing.T, doc any) (map[string]any, map[string]any) {
	t.Helper()

	root, ok := doc.(map[string]any)
	if !ok {
		t.Fatalf("decoded root type mismatch: %T", doc)
	}

	spec, ok := root["spec"].(map[string]any)
	if !ok {
		t.Fatalf("decoded spec type mismatch: %T", root["spec"])
	}

	hooks, ok := spec["hooks"].([]any)
	if !ok || len(hooks) == 0 {
		t.Fatalf("decoded hooks type mismatch: %T", spec["hooks"])
	}

	steps, ok := hooks[0].([]any)
	if !ok || len(steps) == 0 {
		t.Fatalf("decoded hook steps type mismatch: %T", hooks[0])
	}

	step, ok := steps[0].(map[string]any)
	if !ok {
		t.Fatalf("decoded hook step mismatch: %T", steps[0])
	}

	return spec, step
}
