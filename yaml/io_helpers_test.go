package yaml

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnmarshalAuto(t *testing.T) {
	t.Parallel()

	type cfg struct {
		A string `json:"a"`
	}

	t.Run("yaml by content", func(t *testing.T) {
		var got cfg
		if err := UnmarshalAuto([]byte("a: x\n"), &got, AutoOptions{}); err != nil {
			t.Fatalf("UnmarshalAuto returned error: %v", err)
		}
		if got.A != "x" {
			t.Fatalf("value mismatch: got %q, want %q", got.A, "x")
		}
	})

	t.Run("json by content", func(t *testing.T) {
		var got cfg
		if err := UnmarshalAuto([]byte(`{"a":"x"}`), &got, AutoOptions{}); err != nil {
			t.Fatalf("UnmarshalAuto returned error: %v", err)
		}
		if got.A != "x" {
			t.Fatalf("value mismatch: got %q, want %q", got.A, "x")
		}
	})

	t.Run("yaml flow style by content", func(t *testing.T) {
		var got cfg
		if err := UnmarshalAuto([]byte("{a: x}\n"), &got, AutoOptions{}); err != nil {
			t.Fatalf("UnmarshalAuto returned error: %v", err)
		}
		if got.A != "x" {
			t.Fatalf("value mismatch: got %q, want %q", got.A, "x")
		}
	})

	t.Run("json with trailing garbage fails", func(t *testing.T) {
		var got cfg
		err := UnmarshalAuto([]byte(`{"a":"x"} trailing`), &got, AutoOptions{})
		if err == nil {
			t.Fatal("expected trailing-data error")
		}
		if !strings.Contains(err.Error(), "trailing data") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestReadFile(t *testing.T) {
	t.Parallel()

	type cfg struct {
		A string `json:"a"`
	}

	t.Run("format by extension", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.json")
		if err := os.WriteFile(path, []byte(`{"a":"ok"}`), 0o644); err != nil {
			t.Fatalf("WriteFile setup failed: %v", err)
		}

		var got cfg
		if err := ReadFile(path, &got, ReadOptions{Format: FormatAuto}); err != nil {
			t.Fatalf("ReadFile returned error: %v", err)
		}
		if got.A != "ok" {
			t.Fatalf("value mismatch: got %q, want %q", got.A, "ok")
		}
	})

	t.Run("strict json", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.json")
		if err := os.WriteFile(path, []byte(`{"unknown":"x"}`), 0o644); err != nil {
			t.Fatalf("WriteFile setup failed: %v", err)
		}

		var got cfg
		err := ReadFile(path, &got, ReadOptions{Format: FormatJSON, Strict: true})
		if err == nil {
			t.Fatal("expected strict json error")
		}
		if !strings.Contains(err.Error(), "unknown field") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("strict auto keeps json error", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.auto")
		if err := os.WriteFile(path, []byte(`{"unknown":"x"}`), 0o644); err != nil {
			t.Fatalf("WriteFile setup failed: %v", err)
		}

		var got cfg
		err := ReadFile(path, &got, ReadOptions{Format: FormatAuto, Strict: true})
		if err == nil {
			t.Fatal("expected strict auto json error")
		}
		if !strings.Contains(err.Error(), "unknown field") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("strict yaml unsupported", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")
		if err := os.WriteFile(path, []byte("a: ok\n"), 0o644); err != nil {
			t.Fatalf("WriteFile setup failed: %v", err)
		}

		var got cfg
		err := ReadFile(path, &got, ReadOptions{Format: FormatYAML, Strict: true})
		if !errors.Is(err, ErrStrictYAMLUnsupported) {
			t.Fatalf("expected ErrStrictYAMLUnsupported, got: %v", err)
		}
	})
}

func TestWriteFileAndMarshalWith(t *testing.T) {
	t.Parallel()

	type cfg struct {
		A string `json:"a"`
	}

	input := cfg{A: "ok"}

	t.Run("json with indent", func(t *testing.T) {
		out, err := MarshalWith(input, WriteOptions{Format: FormatJSON, Indent: 2})
		if err != nil {
			t.Fatalf("MarshalWith returned error: %v", err)
		}
		if !strings.Contains(string(out), "\n  \"a\": \"ok\"\n") {
			t.Fatalf("unexpected json output: %q", string(out))
		}
	})

	t.Run("yaml write file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")

		if err := WriteFile(path, input, WriteOptions{Format: FormatYAML, Indent: 2}); err != nil {
			t.Fatalf("WriteFile returned error: %v", err)
		}

		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile failed: %v", err)
		}
		if !strings.Contains(string(raw), "a: ok") {
			t.Fatalf("unexpected yaml output: %q", string(raw))
		}
	})

	t.Run("overwrite existing file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.json")
		if err := os.WriteFile(path, []byte(`{"old":true}`), 0o644); err != nil {
			t.Fatalf("setup write failed: %v", err)
		}

		if err := WriteFile(path, input, WriteOptions{
			Format: FormatJSON,
			Indent: 2,
		}); err != nil {
			t.Fatalf("WriteFile returned error: %v", err)
		}

		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile failed: %v", err)
		}
		if strings.Contains(string(raw), `"old"`) {
			t.Fatalf("old content remained after overwrite: %q", string(raw))
		}
		if !strings.Contains(string(raw), `"a": "ok"`) {
			t.Fatalf("new content missing: %q", string(raw))
		}
	})
}

func TestInvalidFormat(t *testing.T) {
	t.Parallel()

	type cfg struct {
		A string `json:"a"`
	}

	_, err := MarshalWith(cfg{A: "x"}, WriteOptions{Format: Format("toml")})
	if !errors.Is(err, ErrInvalidFormat) {
		t.Fatalf("expected ErrInvalidFormat, got: %v", err)
	}
}
