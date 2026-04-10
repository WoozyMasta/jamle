package jamle

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestUnmarshal_SubstitutionLogic(t *testing.T) {
	tests := []struct {
		name        string
		yaml        string
		env         map[string]string
		expected    map[string]interface{}
		expectError bool
	}{
		// Basic
		{
			name:     "basic substitution",
			yaml:     `value: "${TEST_VAR}"`,
			env:      map[string]string{"TEST_VAR": "val"},
			expected: map[string]interface{}{"value": "val"},
		},
		{
			name:     "basic missing (empty)",
			yaml:     `value: "${TEST_MISSING}"`,
			expected: map[string]interface{}{"value": ""},
		},
		{
			name:     "multiple vars in one line",
			yaml:     `url: "${PROTO:-http}://${HOST:-localhost}:${PORT:-80}"`,
			env:      map[string]string{"HOST": "example.com"},
			expected: map[string]interface{}{"url": "http://example.com:80"},
		},

		// Defaults
		{
			name:     "bash default (:-)",
			yaml:     `value: "${TEST_VAR:-default}"`,
			expected: map[string]interface{}{"value": "default"},
		},
		{
			name:     "colon without operator does not apply default",
			yaml:     `value: "${TEST_VAR:default}"`,
			expected: map[string]interface{}{"value": ""},
		},
		{
			name:     "default with special chars and colons",
			yaml:     `dsn: "${DSN:-user:pass@tcp(localhost:3306)/db}"`,
			expected: map[string]interface{}{"dsn": "user:pass@tcp(localhost:3306)/db"},
		},

		// Required (?)
		{
			name:     "required variable success",
			yaml:     `value: "${TEST_REQ:?}"`,
			env:      map[string]string{"TEST_REQ": "ok"},
			expected: map[string]interface{}{"value": "ok"},
		},
		{
			name:        "required variable fail",
			yaml:        `value: "${TEST_REQ:?}"`,
			expectError: true,
		},
		{
			name:        "required variable fail with custom message",
			yaml:        `value: "${TEST_REQ:?error msg}"`,
			expectError: true,
		},

		// Recursion / Nesting
		{
			name:     "nested default value",
			yaml:     `value: "${PRIMARY:-${SECONDARY:-final}}"`,
			expected: map[string]interface{}{"value": "final"},
		},
		{
			name:     "nested default value resolved",
			yaml:     `value: "${PRIMARY:-${SECONDARY:-final}}"`,
			env:      map[string]string{"SECONDARY": "sec"},
			expected: map[string]interface{}{"value": "sec"},
		},

		// Escaping
		{
			name:     "escaped variable",
			yaml:     `value: "$${TEST_VAR}"`,
			env:      map[string]string{"TEST_VAR": "should_be_ignored"},
			expected: map[string]interface{}{"value": "${TEST_VAR}"},
		},
		{
			name:     "escaped variable inside default value",
			yaml:     `query: "${QUERY:-rate(http_requests[$${INTERVAL}])}"`,
			expected: map[string]interface{}{"query": "rate(http_requests[${INTERVAL}])"},
		},
		{
			name:     "escaped variable keeps nested braces literal",
			yaml:     `value: "$${A:-${B}}"`,
			expected: map[string]interface{}{"value": "${A:-${B}}"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			var result map[string]interface{}
			err := Unmarshal([]byte(tt.yaml), &result)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			// Simple check
			for k, v := range tt.expected {
				if result[k] != v {
					t.Errorf("Key %q: expected %v, got %v", k, v, result[k])
				}
			}
		})
	}
}

func TestUnmarshal_AssignmentOperator(t *testing.T) {
	const varName = "TEST_ASSIGN_X"

	t.Run("assigns if missing", func(t *testing.T) {
		_ = os.Unsetenv(varName)
		t.Cleanup(func() { _ = os.Unsetenv(varName) })

		yamlStr := `val: "${TEST_ASSIGN_X:=assigned}"`
		var res map[string]string

		if err := Unmarshal([]byte(yamlStr), &res); err != nil {
			t.Fatal(err)
		}

		if res["val"] != "assigned" {
			t.Errorf("Expected result 'assigned', got %q", res["val"])
		}
		if env := os.Getenv(varName); env != "assigned" {
			t.Errorf("Expected env var to be set to 'assigned', got %q", env)
		}
	})

	t.Run("does not assign if present", func(t *testing.T) {
		t.Setenv(varName, "existing")

		yamlStr := `val: "${TEST_ASSIGN_X:=new}"`
		var res map[string]string

		if err := Unmarshal([]byte(yamlStr), &res); err != nil {
			t.Fatal(err)
		}

		if res["val"] != "existing" {
			t.Errorf("Expected result 'existing', got %q", res["val"])
		}
		if env := os.Getenv(varName); env != "existing" {
			t.Errorf("Env var changed unexpectedly to %q", env)
		}
	})
}

func TestUnmarshal_TypePreservation(t *testing.T) {
	yamlStr := `
integer: ${INT_VAL:-42}
float: ${FLOAT_VAL:-42.5}
bool: ${BOOL_VAL:-true}
string_num: "${STR_VAL:-123}"
`

	type Config struct {
		Integer   int     `json:"integer"`
		Float     float64 `json:"float"`
		Bool      bool    `json:"bool"`
		StringNum string  `json:"string_num"`
	}

	var cfg Config
	if err := Unmarshal([]byte(yamlStr), &cfg); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if cfg.Integer != 42 {
		t.Errorf("Expected int 42, got %d", cfg.Integer)
	}
	if cfg.Float != 42.5 {
		t.Errorf("Expected float 42.5, got %f", cfg.Float)
	}
	if cfg.Bool != true {
		t.Errorf("Expected bool true, got %v", cfg.Bool)
	}
	if cfg.StringNum != "123" {
		t.Errorf("Expected string '123', got %q", cfg.StringNum)
	}
}

func TestUnmarshal_InfiniteLoopProtection(t *testing.T) {
	yamlStr := `val: "${RECURSIVE_VAR:-${RECURSIVE_VAR}}"`

	var res map[string]interface{}

	done := make(chan bool)
	go func() {
		_ = Unmarshal([]byte(yamlStr), &res)
		done <- true
	}()

	select {
	case <-done:
		// Success (didn't hang)
	case <-time.After(1 * time.Second):
		t.Fatal("Unmarshal timed out, possible infinite loop")
	}
}

func TestUnmarshal_DoesNotExpandInComments(t *testing.T) {
	os.Unsetenv("COMMENT_REQ")

	yamlStr := `
# ${COMMENT_REQ:?must_not_fail}
value: "ok" # ${COMMENT_REQ:?must_not_fail_inline}
nested:
  key: 1 # ${COMMENT_REQ:?must_not_fail_nested}
`

	var res map[string]interface{}
	if err := Unmarshal([]byte(yamlStr), &res); err != nil {
		t.Fatalf("Unexpected error (comments must not be expanded): %v", err)
	}
	if res["value"] != "ok" {
		t.Fatalf("Expected value=ok, got %v", res["value"])
	}
}

func TestUnmarshalAll(t *testing.T) {
	type doc struct {
		Port int    `json:"port"`
		Host string `json:"host"`
	}

	t.Run("multiple docs with env", func(t *testing.T) {
		t.Setenv("HOST_A", "a.local")

		input := []byte(`
---
port: 8080
host: ${HOST_A:-localhost}
---
port: 9090
host: ${HOST_B:-default.local}
`)

		var got []doc
		if err := UnmarshalAll(input, &got); err != nil {
			t.Fatalf("UnmarshalAll returned error: %v", err)
		}

		if len(got) != 2 {
			t.Fatalf("expected 2 documents, got %d", len(got))
		}
		if got[0].Port != 8080 || got[0].Host != "a.local" {
			t.Fatalf("first doc mismatch: %#v", got[0])
		}
		if got[1].Port != 9090 || got[1].Host != "default.local" {
			t.Fatalf("second doc mismatch: %#v", got[1])
		}
	})

	t.Run("invalid out type", func(t *testing.T) {
		var notSlice doc
		err := UnmarshalAll([]byte("port: 1\n"), &notSlice)
		if err == nil {
			t.Fatal("expected out type error")
		}
	})
}

type mapResolver struct {
	values map[string]string
}

func (r mapResolver) Lookup(name string) (string, bool) {
	v, ok := r.values[name]
	return v, ok
}

type mapResolverWithSet struct {
	values map[string]string
}

func (r mapResolverWithSet) Lookup(name string) (string, bool) {
	v, ok := r.values[name]
	return v, ok
}

func (r mapResolverWithSet) Set(name, value string) error {
	r.values[name] = value
	return nil
}

func TestUnmarshalWithOptions_CustomResolver(t *testing.T) {
	type cfg struct {
		A string `json:"a"`
		B string `json:"b"`
	}

	t.Run("custom resolver lookup", func(t *testing.T) {
		input := []byte("a: ${A}\nb: ${B:-default}\n")
		resolver := mapResolver{
			values: map[string]string{"A": "from-resolver"},
		}

		var got cfg
		if err := UnmarshalWithOptions(input, &got, UnmarshalOptions{
			Resolver: resolver,
		}); err != nil {
			t.Fatalf("UnmarshalWithOptions returned error: %v", err)
		}

		if got.A != "from-resolver" || got.B != "default" {
			t.Fatalf("decoded config mismatch: %#v", got)
		}
	})

	t.Run("assignment unsupported without setter", func(t *testing.T) {
		input := []byte("a: ${A:=value}\n")
		resolver := mapResolver{values: map[string]string{}}

		var got cfg
		err := UnmarshalWithOptions(input, &got, UnmarshalOptions{
			Resolver: resolver,
		})
		if err == nil {
			t.Fatal("expected assignment unsupported error")
		}

		if !errors.Is(err, ErrAssignmentUnsupported) {
			t.Fatalf("expected ErrAssignmentUnsupported, got: %v", err)
		}
	})
}

func TestUnmarshalWithOptions(t *testing.T) {
	type cfg struct {
		A string `json:"a"`
	}

	t.Run("disable assignment side effect", func(t *testing.T) {
		const key = "JAMLE_DISABLE_ASSIGNMENT_TEST"
		_ = os.Unsetenv(key)
		t.Cleanup(func() { _ = os.Unsetenv(key) })

		var got cfg
		err := UnmarshalWithOptions([]byte("a: ${JAMLE_DISABLE_ASSIGNMENT_TEST:=value}\n"), &got, UnmarshalOptions{
			DisableAssignment: true,
		})
		if err != nil {
			t.Fatalf("UnmarshalWithOptions returned error: %v", err)
		}
		if got.A != "value" {
			t.Fatalf("decoded value mismatch: got %q, want %q", got.A, "value")
		}
		if _, exists := os.LookupEnv(key); exists {
			t.Fatalf("expected %s to stay unset", key)
		}
	})

	t.Run("max passes limits nested expansion", func(t *testing.T) {
		input := []byte(`a: "${A:-${B:-final}}"`)

		var got cfg
		err := UnmarshalWithOptions(input, &got, UnmarshalOptions{
			MaxPasses: 1,
		})
		if err != nil {
			t.Fatalf("UnmarshalWithOptions returned error: %v", err)
		}
		if got.A != "${A:-final}" {
			t.Fatalf("expected unresolved outer expression, got %q", got.A)
		}
	})

	t.Run("disable required errors behaves like plain variable", func(t *testing.T) {
		type reqCfg struct {
			A string `json:"a"`
			B string `json:"b"`
		}

		t.Setenv("JAMLE_REQUIRED_EMPTY", "")
		input := []byte("a: ${JAMLE_REQUIRED_MISSING:?must fail}\nb: ${JAMLE_REQUIRED_EMPTY:?must fail}\n")

		var got reqCfg
		err := UnmarshalWithOptions(input, &got, UnmarshalOptions{
			DisableRequiredErrors: true,
		})
		if err != nil {
			t.Fatalf("UnmarshalWithOptions returned error: %v", err)
		}
		if got.A != "" || got.B != "" {
			t.Fatalf("expected empty values for plain-variable behavior, got: %#v", got)
		}
	})
}

func TestUnmarshalWithOptions_IgnoreExpandPaths(t *testing.T) {
	type hookStep struct {
		Script string `json:"script"`
		Args   string `json:"args"`
	}

	type cfg struct {
		Spec struct {
			Hooks   [][]hookStep `json:"hooks"`
			Token   string       `json:"token"`
			Escaped string       `json:"escaped"`
		} `json:"spec"`
	}

	input := []byte(`
spec:
  hooks:
    - - script: "echo ${HOOK_SCRIPT:-raw}"
        args: "${HOOK_ARG:-default-arg}"
    - - script: "echo ${POST_SCRIPT:-post-raw}"
        args: "${POST_ARG:-post-arg}"
  token: "${TOKEN:-token-v}"
  escaped: "$${LITERAL_TOKEN}"
`)

	var got cfg
	err := UnmarshalWithOptions(input, &got, UnmarshalOptions{
		IgnoreExpandPaths: []string{"spec.hooks.*.*.script"},
	})
	if err != nil {
		t.Fatalf("UnmarshalWithOptions returned error: %v", err)
	}

	if got.Spec.Hooks[0][0].Script != "echo ${HOOK_SCRIPT:-raw}" {
		t.Fatalf("script path must stay untouched, got %q", got.Spec.Hooks[0][0].Script)
	}
	if got.Spec.Hooks[1][0].Script != "echo ${POST_SCRIPT:-post-raw}" {
		t.Fatalf("script path must stay untouched, got %q", got.Spec.Hooks[1][0].Script)
	}
	if got.Spec.Hooks[0][0].Args != "default-arg" {
		t.Fatalf("args must be expanded, got %q", got.Spec.Hooks[0][0].Args)
	}
	if got.Spec.Hooks[1][0].Args != "post-arg" {
		t.Fatalf("args must be expanded, got %q", got.Spec.Hooks[1][0].Args)
	}
	if got.Spec.Token != "token-v" {
		t.Fatalf("token must be expanded, got %q", got.Spec.Token)
	}
	if got.Spec.Escaped != "${LITERAL_TOKEN}" {
		t.Fatalf("escaped placeholder mismatch: got %q", got.Spec.Escaped)
	}
}

func TestUnmarshalWithOptions_NoExpandTag(t *testing.T) {
	type hookStep struct {
		Script string `json:"script" jamle:"noexpand"`
		Args   string `json:"args"`
	}

	type cfg struct {
		Spec struct {
			Hooks [][]hookStep `json:"hooks"`
			Token string       `json:"token"`
		} `json:"spec"`
	}

	input := []byte(`
spec:
  hooks:
    - - script: "echo ${HOOK_SCRIPT:-raw}"
        args: "${HOOK_ARG:-default-arg}"
  token: "${TOKEN:-token-v}"
`)

	var got cfg
	err := UnmarshalWithOptions(input, &got, UnmarshalOptions{})
	if err != nil {
		t.Fatalf("UnmarshalWithOptions returned error: %v", err)
	}

	if got.Spec.Hooks[0][0].Script != "echo ${HOOK_SCRIPT:-raw}" {
		t.Fatalf("noexpand script mismatch: got %q", got.Spec.Hooks[0][0].Script)
	}
	if got.Spec.Hooks[0][0].Args != "default-arg" {
		t.Fatalf("args must still expand, got %q", got.Spec.Hooks[0][0].Args)
	}
	if got.Spec.Token != "token-v" {
		t.Fatalf("token must still expand, got %q", got.Spec.Token)
	}
}

func TestUnmarshalWithOptions_NoExpandTag_YAMLSkipDoesNotLeakPath(t *testing.T) {
	type cfg struct {
		Ignored string `yaml:"-" jamle:"noexpand"`
		Value   string `json:"ignored"`
	}

	input := []byte("ignored: \"${SAME_KEY:-expanded}\"\n")

	var got cfg
	if err := UnmarshalWithOptions(input, &got, UnmarshalOptions{}); err != nil {
		t.Fatalf("UnmarshalWithOptions returned error: %v", err)
	}

	if got.Value != "expanded" {
		t.Fatalf("value must be expanded, got %q", got.Value)
	}
}

func TestUnmarshalWithOptions_NegativeTable(t *testing.T) {
	type cfg struct {
		A string `json:"a"`
	}

	tests := []struct {
		name    string
		input   []byte
		target  any
		opts    UnmarshalOptions
		wantErr string
	}{
		{
			name:   "assignment operator unsupported",
			input:  []byte("a: ${A:=value}\n"),
			target: &cfg{},
			opts: UnmarshalOptions{
				Resolver: mapResolver{values: map[string]string{}},
			},
			wantErr: ErrAssignmentUnsupported.Error(),
		},
		{
			name:   "invalid yaml input",
			input:  []byte("{a: 1"),
			target: &cfg{},
			opts: UnmarshalOptions{
				Resolver: mapResolver{values: map[string]string{}},
			},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			err := UnmarshalWithOptions(tt.input, tt.target, tt.opts)
			if err == nil {
				t.Fatalf("expected error")
			}

			if tt.wantErr == "" {
				return
			}

			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestUnmarshalAllWithOptions_CustomResolver(t *testing.T) {
	type doc struct {
		Host string `json:"host"`
	}

	t.Run("custom resolver for all docs", func(t *testing.T) {
		input := []byte(`
---
host: ${A:-default-a}
---
host: ${B:-default-b}
`)
		resolver := mapResolver{values: map[string]string{"A": "from-a"}}

		var got []doc
		if err := UnmarshalAllWithOptions(input, &got, UnmarshalOptions{
			Resolver: resolver,
		}); err != nil {
			t.Fatalf("UnmarshalAllWithOptions returned error: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("expected 2 documents, got %d", len(got))
		}
		if got[0].Host != "from-a" || got[1].Host != "default-b" {
			t.Fatalf("decoded docs mismatch: %#v", got)
		}
	})

	t.Run("assignment with setter works", func(t *testing.T) {
		input := []byte(`
---
host: ${A:=set-a}
---
host: ${A}
`)
		resolver := mapResolverWithSet{values: map[string]string{}}

		var got []doc
		if err := UnmarshalAllWithOptions(input, &got, UnmarshalOptions{
			Resolver: resolver,
		}); err != nil {
			t.Fatalf("UnmarshalAllWithOptions returned error: %v", err)
		}
		if got[0].Host != "set-a" || got[1].Host != "set-a" {
			t.Fatalf("assignment docs mismatch: %#v", got)
		}
	})
}

func TestUnmarshalAllWithOptions_NegativeTable(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		out     any
		opts    UnmarshalOptions
		wantErr string
	}{
		{
			name:  "invalid out type",
			input: []byte("---\na: 1\n"),
			out:   &map[string]any{},
			opts: UnmarshalOptions{
				Resolver: mapResolver{values: map[string]string{}},
			},
			wantErr: ErrOutMustBePointerToSlice.Error(),
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			err := UnmarshalAllWithOptions(tt.input, tt.out, tt.opts)
			if err == nil {
				t.Fatalf("expected error")
			}

			switch tt.wantErr {
			case ErrOutMustBePointerToSlice.Error():
				if !errors.Is(err, ErrOutMustBePointerToSlice) {
					t.Fatalf("expected ErrOutMustBePointerToSlice, got: %v", err)
				}
			default:
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestUnmarshal_JSONInputWithEnvExpansion(t *testing.T) {
	t.Setenv("JAMLE_JSON_HOST", "json.local")
	t.Setenv("JAMLE_JSON_PORT", "8080")
	t.Setenv("JAMLE_JSON_ENABLED", "true")

	input := []byte(`{
  "host": "${JAMLE_JSON_HOST}",
  "port": "${JAMLE_JSON_PORT}",
  "enabled": "${JAMLE_JSON_ENABLED}"
}`)

	type cfg struct {
		Host    string `json:"host"`
		Port    string `json:"port"`
		Enabled string `json:"enabled"`
	}

	var got cfg
	if err := Unmarshal(input, &got); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}

	if got.Host != "json.local" {
		t.Fatalf("host mismatch: got %q, want %q", got.Host, "json.local")
	}
	if got.Port != "8080" {
		t.Fatalf("port mismatch: got %q, want %q", got.Port, "8080")
	}
	if got.Enabled != "true" {
		t.Fatalf("enabled mismatch: got %q, want %q", got.Enabled, "true")
	}
}

func TestUnmarshal_JSONInputWithDefaultsInStrings(t *testing.T) {
	input := []byte(`{
  "host": "${JAMLE_JSON_HOST_MISSING:-localhost}",
  "port": "${JAMLE_JSON_PORT_MISSING:-8080}"
}`)

	type cfg struct {
		Host string `json:"host"`
		Port string `json:"port"`
	}

	var got cfg
	if err := Unmarshal(input, &got); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}

	if got.Host != "localhost" || got.Port != "8080" {
		t.Fatalf("defaults mismatch: %#v", got)
	}
}

func TestUnmarshal_JSONInputWithUnquotedPlaceholdersFails(t *testing.T) {
	t.Setenv("JAMLE_JSON_PORT", "8080")

	input := []byte(`{
  "port": ${JAMLE_JSON_PORT}
}`)

	var got map[string]any
	if err := Unmarshal(input, &got); err == nil {
		t.Fatal("expected parse error for unquoted placeholder in JSON")
	}
}
