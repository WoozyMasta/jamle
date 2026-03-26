package yaml

import "testing"

type conversionCase struct {
	name    string
	input   string
	output  string
	reverse *string
}

type conversionDirection int

const (
	runJSONToYAML conversionDirection = iota
	runYAMLToJSON
)

func TestJSONToYAML(t *testing.T) {
	t.Parallel()

	cases := []conversionCase{
		{
			name:   "string value",
			input:  `{"t":"a"}`,
			output: "t: a\n",
		},
		{
			name:   "null value",
			input:  `{"t":null}`,
			output: "t: null\n",
		},
	}

	runConversionCases(t, runJSONToYAML, cases)
}

func TestYAMLToJSON(t *testing.T) {
	t.Parallel()

	cases := []conversionCase{
		{name: "simple string", input: "t: a\n", output: `{"t":"a"}`},
		{name: "empty value", input: "t: \n", output: `{"t":null}`, reverse: strPtr("t: null\n")},
		{name: "explicit null", input: "t: null\n", output: `{"t":null}`},
		{name: "bool-like true key", input: "true: yes\n", output: `{"true":"yes"}`, reverse: strPtr("\"true\": \"yes\"\n")},
		{name: "bool-like false key", input: "false: yes\n", output: `{"false":"yes"}`, reverse: strPtr("\"false\": \"yes\"\n")},
		{name: "int key", input: "1: a\n", output: `{"1":"a"}`, reverse: strPtr("\"1\": a\n")},
		{name: "big number key", input: "1000000000000000000000000000000000000: a\n", output: `{"1e+36":"a"}`, reverse: strPtr("\"1e+36\": a\n")},
		{name: "scientific key", input: "1e+36: a\n", output: `{"1e+36":"a"}`, reverse: strPtr("\"1e+36\": a\n")},
		{name: "quoted scientific key", input: "\"1e+36\": a\n", output: `{"1e+36":"a"}`},
		{name: "quoted decimal key", input: "\"1.2\": a\n", output: `{"1.2":"a"}`},
		{name: "list of maps", input: "- t: a\n", output: `[{"t":"a"}]`},
		{
			name: "list mixed values",
			input: "- t: a\n" +
				"- t:\n" +
				"    b: 1\n" +
				"    c: 2\n",
			output: `[{"t":"a"},{"t":{"b":1,"c":2}}]`,
		},
		{
			name:   "flow list maps",
			input:  `[{t: a}, {t: {b: 1, c: 2}}]`,
			output: `[{"t":"a"},{"t":{"b":1,"c":2}}]`,
			reverse: strPtr("- t: a\n" +
				"- t:\n" +
				"    b: 1\n" +
				"    c: 2\n"),
		},
		{name: "null in list", input: "- t: \n", output: `[{"t":null}]`, reverse: strPtr("- t: null\n")},
		{name: "explicit null in list", input: "- t: null\n", output: `[{"t":null}]`},
	}

	runConversionCases(t, runYAMLToJSON, cases)
}

func TestYAMLToJSONDuplicateFields(t *testing.T) {
	t.Parallel()

	data := []byte("foo: bar\nfoo: baz\n")
	if _, err := YAMLToJSON(data); err == nil {
		t.Fatal("expected YAMLToJSON to fail on duplicate field names")
	}
}

func TestYAMLToJSON_Uint64MapKey(t *testing.T) {
	t.Parallel()

	input := []byte("18446744073709551615: a\n")
	got, err := YAMLToJSON(input)
	if err != nil {
		t.Fatalf("YAMLToJSON returned error: %v", err)
	}

	if string(got) != `{"18446744073709551615":"a"}` {
		t.Fatalf("YAMLToJSON mismatch:\n got: %q\nwant: %q", string(got), `{"18446744073709551615":"a"}`)
	}
}

func runConversionCases(t *testing.T, direction conversionDirection, cases []conversionCase) {
	t.Helper()

	var forward func([]byte) ([]byte, error)
	var backward func([]byte) ([]byte, error)
	var forwardMsg string
	var backwardMsg string

	if direction == runJSONToYAML {
		forward = JSONToYAML
		backward = YAMLToJSON
		forwardMsg = "JSON to YAML"
		backwardMsg = "YAML back to JSON"
	} else {
		forward = YAMLToJSON
		backward = JSONToYAML
		forwardMsg = "YAML to JSON"
		backwardMsg = "JSON back to YAML"
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			forwardOut, err := forward([]byte(tc.input))
			if err != nil {
				t.Fatalf("%s failed for %q: %v", forwardMsg, tc.input, err)
			}
			if string(forwardOut) != tc.output {
				t.Fatalf("%s mismatch:\n got: %q\nwant: %q", forwardMsg, string(forwardOut), tc.output)
			}

			wantReverse := tc.input
			if tc.reverse != nil {
				wantReverse = *tc.reverse
			}

			backwardOut, err := backward(forwardOut)
			if err != nil {
				t.Fatalf("%s failed for %q: %v", backwardMsg, string(forwardOut), err)
			}
			if string(backwardOut) != wantReverse {
				t.Fatalf("%s mismatch:\n got: %q\nwant: %q", backwardMsg, string(backwardOut), wantReverse)
			}
		})
	}
}
