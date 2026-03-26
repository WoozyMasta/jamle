package yaml

import (
	"reflect"
	"strings"
	"testing"
)

type unmarshalString struct {
	A string
	B string
}

type unmarshalStringMap struct {
	A map[string]string
}

type unmarshalNestedString struct {
	A nestedString
}

type nestedString struct {
	A string
}

type unmarshalSlice struct {
	A []nestedSlice
}

type nestedSlice struct {
	B string
	C *string
}

type unmarshalCase struct {
	name      string
	inputYAML []byte
	newTarget func() any
	want      any
	wantErr   string
}

func TestUnmarshal(t *testing.T) {
	t.Parallel()

	type namedThing struct {
		Name string `json:"name"`
	}

	cases := []unmarshalCase{
		{
			name:      "empty yaml",
			inputYAML: []byte(""),
			newTarget: func() any { return &unmarshalString{} },
			want:      &unmarshalString{},
		},
		{
			name:      "empty map",
			inputYAML: []byte("{}"),
			newTarget: func() any { return &unmarshalString{} },
			want:      &unmarshalString{},
		},
		{
			name:      "number into string",
			inputYAML: []byte("a: 1"),
			newTarget: func() any { return &unmarshalString{} },
			want:      &unmarshalString{A: "1"},
		},
		{
			name:      "quoted string",
			inputYAML: []byte(`a: "1"`),
			newTarget: func() any { return &unmarshalString{} },
			want:      &unmarshalString{A: "1"},
		},
		{
			name:      "bool into string",
			inputYAML: []byte("a: true"),
			newTarget: func() any { return &unmarshalString{} },
			want:      &unmarshalString{A: "true"},
		},
		{
			name:      "nested struct",
			inputYAML: []byte("a:\n  a: 1"),
			newTarget: func() any { return &unmarshalNestedString{} },
			want:      &unmarshalNestedString{A: nestedString{A: "1"}},
		},
		{
			name: "slice of structs",
			inputYAML: []byte(
				"a:\n  - b: abc\n    c: def\n  - b: 123\n    c: 456\n",
			),
			newTarget: func() any { return &unmarshalSlice{} },
			want: &unmarshalSlice{
				A: []nestedSlice{
					{B: "abc", C: strPtr("def")},
					{B: "123", C: strPtr("456")},
				},
			},
		},
		{
			name:      "map value into string",
			inputYAML: []byte("a:\n  b: 1"),
			newTarget: func() any { return &unmarshalStringMap{} },
			want:      &unmarshalStringMap{A: map[string]string{"b": "1"}},
		},
		{
			name: "map string to pointer struct",
			inputYAML: []byte(`
a:
  name: TestA
b:
  name: TestB
`),
			newTarget: func() any { return &map[string]*namedThing{} },
			want: &map[string]*namedThing{
				"a": {Name: "TestA"},
				"b": {Name: "TestB"},
			},
		},
	}

	runUnmarshalCases(t, cases)
}

func TestUnmarshalNonStrict(t *testing.T) {
	t.Parallel()

	cases := []unmarshalCase{
		{
			name:      "plain scalar to string",
			inputYAML: []byte("a: 1"),
			newTarget: func() any { return &unmarshalString{} },
			want:      &unmarshalString{A: "1"},
		},
		{
			name:      "order does not matter",
			inputYAML: []byte("b: 1\na: 2"),
			newTarget: func() any { return &unmarshalString{} },
			want:      &unmarshalString{A: "2", B: "1"},
		},
		{
			name:      "unknown field ignored",
			inputYAML: []byte("a: 1\nunknownField: 2"),
			newTarget: func() any { return &unmarshalString{} },
			want:      &unmarshalString{A: "1"},
		},
		{
			name:      "multiple unknown fields ignored",
			inputYAML: []byte("unknownOne: 2\na: 1\nunknownTwo: 2"),
			newTarget: func() any { return &unmarshalString{} },
			want:      &unmarshalString{A: "1"},
		},
		{
			name:      "yaml yes stays string",
			inputYAML: []byte("a: YES"),
			newTarget: func() any { return &unmarshalString{} },
			want:      &unmarshalString{A: "YES"},
		},
	}

	runUnmarshalCases(t, cases)
}

func TestUnmarshalErrors(t *testing.T) {
	t.Parallel()

	cases := []unmarshalCase{
		{
			name:      "duplicate string key",
			inputYAML: []byte("a: 1\na: 2"),
			newTarget: func() any { return &unmarshalString{} },
			wantErr:   `key "a" already defined`,
		},
		{
			name:      "duplicate key with different shape",
			inputYAML: []byte("a: [1,2,3]\na: value-of-a"),
			newTarget: func() any { return &unmarshalString{} },
			wantErr:   `key "a" already defined`,
		},
		{
			name:      "duplicate bool-like key",
			inputYAML: []byte("true: string-value-of-yes\ntrue: 1"),
			newTarget: func() any { return &unmarshalString{} },
			wantErr:   `key "true" already defined`,
		},
	}

	runUnmarshalCases(t, cases)
}

func runUnmarshalCases(t *testing.T, cases []unmarshalCase) {
	t.Helper()

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			target := tc.newTarget()
			err := Unmarshal(tc.inputYAML, target)

			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("Unmarshal(%q) expected error containing %q", string(tc.inputYAML), tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("Unmarshal(%q) error mismatch:\n got: %v\nwant: %q", string(tc.inputYAML), err, tc.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("Unmarshal(%q) returned error: %v", string(tc.inputYAML), err)
			}

			if !reflect.DeepEqual(target, tc.want) {
				t.Fatalf("Unmarshal(%q) mismatch:\n got: %#v\nwant: %#v", string(tc.inputYAML), target, tc.want)
			}
		})
	}
}

func strPtr(s string) *string {
	return &s
}
