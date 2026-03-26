package yaml

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"testing"
)

type marshalSample struct {
	A string
	B int64
	C float32
	D float64
}

func TestMarshal(t *testing.T) {
	t.Parallel()

	f32String := strconv.FormatFloat(math.MaxFloat32, 'g', -1, 32)
	f64String := strconv.FormatFloat(math.MaxFloat64, 'g', -1, 64)
	input := marshalSample{"a", math.MaxInt64, math.MaxFloat32, math.MaxFloat64}
	want := []byte(fmt.Sprintf(
		"A: a\nB: %d\nC: %s\nD: %s\n",
		int64(math.MaxInt64),
		f32String,
		f64String,
	))

	got, err := Marshal(input)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Marshal mismatch:\n got: %q\nwant: %q", string(got), string(want))
	}
}

func TestMarshal_WrapsJSONError(t *testing.T) {
	t.Parallel()

	type bad struct {
		C chan int `json:"c"`
	}

	_, err := Marshal(bad{C: make(chan int)})
	if err == nil {
		t.Fatal("expected Marshal to fail")
	}

	var unsupportedTypeErr *json.UnsupportedTypeError
	if !errors.As(err, &unsupportedTypeErr) {
		t.Fatalf("expected wrapped *json.UnsupportedTypeError, got: %v", err)
	}
}
