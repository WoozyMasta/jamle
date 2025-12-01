package jamle

import (
	"os"
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
			yaml:     `url: "${PROTO:http}://${HOST:localhost}:${PORT:80}"`,
			env:      map[string]string{"HOST": "example.com"},
			expected: map[string]interface{}{"url": "http://example.com:80"},
		},

		// Defaults
		{
			name:     "default value (:)",
			yaml:     `value: "${TEST_VAR:default}"`,
			expected: map[string]interface{}{"value": "default"},
		},
		{
			name:     "bash default (:-)",
			yaml:     `value: "${TEST_VAR:-default}"`,
			expected: map[string]interface{}{"value": "default"},
		},
		{
			name:     "default with special chars and colons",
			yaml:     `dsn: "${DSN:user:pass@tcp(localhost:3306)/db}"`,
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup Env
			for k, v := range tt.env {
				os.Setenv(k, v)
			}
			// Cleanup Env after test
			defer func() {
				for k := range tt.env {
					os.Unsetenv(k)
				}
			}()

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
		os.Unsetenv(varName)
		defer os.Unsetenv(varName)

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
		os.Setenv(varName, "existing")
		defer os.Unsetenv(varName)

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
integer: ${INT_VAL:42}
float: ${FLOAT_VAL:42.5}
bool: ${BOOL_VAL:true}
string_num: "${STR_VAL:123}"
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
	yamlStr := `val: "${RECURSIVE_VAR:${RECURSIVE_VAR}}"`

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
