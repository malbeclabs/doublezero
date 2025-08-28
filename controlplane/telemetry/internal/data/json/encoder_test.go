package datajson

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func decUseNumber(t *testing.T, s string) any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return v
}

func TestTopLevelObject_FilterDeep_NumbersAndLists(t *testing.T) {
	raw := `{"a":1,"b":2.5,"c":9223372036854775807,"nested":{"x":1,"y":[1,2,3]},"arr":[{"k":1},{"k":2}], "z":null}`
	var out bytes.Buffer
	e := NewFieldFilteringEncoder(&out, []string{"c", "arr"})
	if err := e.EncodeReader(strings.NewReader(raw)); err != nil {
		t.Fatalf("EncodeReader: %v", err)
	}

	v := decUseNumber(t, out.String()).(map[string]any)
	if _, ok := v["a"]; ok {
		t.Fatal("a should be filtered")
	}
	if _, ok := v["b"]; ok {
		t.Fatal("b should be filtered")
	}
	if _, ok := v["nested"]; ok {
		t.Fatal("nested should be filtered")
	}
	if _, ok := v["z"]; ok {
		t.Fatal("z should be filtered")
	}

	c, ok := v["c"].(json.Number)
	if !ok || c.String() != "9223372036854775807" {
		t.Fatalf("c not preserved as json.Number: %#v", v["c"])
	}
	arr, ok := v["arr"].([]any)
	if !ok || len(arr) != 2 {
		t.Fatalf("arr bad: %#v", v["arr"])
	}
	// Deep filtering: objects inside arr have their fields filtered too,
	// and since "k" isn't allowed, they become empty objects.
	if _, ok := arr[0].(map[string]any); !ok {
		t.Fatalf("arr[0] not an object: %#v", arr[0])
	}
	if _, ok := arr[1].(map[string]any); !ok {
		t.Fatalf("arr[1] not an object: %#v", arr[1])
	}
	if len(arr[0].(map[string]any)) != 0 || len(arr[1].(map[string]any)) != 0 {
		t.Fatalf("nested objects should be empty after filtering: %#v", arr)
	}
}

func TestTopLevelObject_FilterDeep_WithNestedAllowedKey(t *testing.T) {
	raw := `{"keep":true,"arr":[{"k":1,"x":9},{"k":2,"y":8}],"nested":{"k":3,"x":7}}`
	var out bytes.Buffer
	// Allow "arr" and "k" so that inner objects keep only "k" fields.
	e := NewFieldFilteringEncoder(&out, []string{"arr", "k"})
	if err := e.EncodeReader(strings.NewReader(raw)); err != nil {
		t.Fatalf("EncodeReader: %v", err)
	}
	v := decUseNumber(t, out.String()).(map[string]any)

	if _, ok := v["keep"]; ok {
		t.Fatal("keep should be filtered")
	}
	if _, ok := v["nested"]; ok {
		t.Fatal("nested top-level key should be filtered entirely")
	}

	arr, ok := v["arr"].([]any)
	if !ok || len(arr) != 2 {
		t.Fatalf("arr bad: %#v", v["arr"])
	}
	m0 := arr[0].(map[string]any)
	m1 := arr[1].(map[string]any)
	if len(m0) != 1 || len(m1) != 1 {
		t.Fatalf("nested objects should only have 'k': %#v", arr)
	}
	k0, ok0 := m0["k"].(json.Number)
	k1, ok1 := m1["k"].(json.Number)
	if !ok0 || !ok1 || k0.String() != "1" || k1.String() != "2" {
		t.Fatalf("nested k values wrong: %#v", arr)
	}
}

func TestTopLevelArray_FilterElementsDeep(t *testing.T) {
	raw := `[{"i":0},{"i":1},{"i":2},{"i":3}]`
	var out bytes.Buffer
	// No allowed field "i", so each object becomes {}.
	e := NewFieldFilteringEncoder(&out, []string{"unused"})
	if err := e.EncodeReader(strings.NewReader(raw)); err != nil {
		t.Fatalf("EncodeReader: %v", err)
	}
	v := decUseNumber(t, out.String()).([]any)
	if len(v) != 4 {
		t.Fatalf("array length changed: %#v", v)
	}
	for idx, el := range v {
		m, ok := el.(map[string]any)
		if !ok {
			t.Fatalf("element %d not object: %#v", idx, el)
		}
		if len(m) != 0 {
			t.Fatalf("element %d should be empty object: %#v", idx, m)
		}
	}

	// Filtering within object with arrays of primitives remains pass-through for values.
	raw2 := `{"keep":[1,2,3,4],"drop":[9,9]}`
	out.Reset()
	e = NewFieldFilteringEncoder(&out, []string{"keep"})
	if err := e.EncodeReader(strings.NewReader(raw2)); err != nil {
		t.Fatalf("EncodeReader: %v", err)
	}
	m := decUseNumber(t, out.String()).(map[string]any)
	if _, ok := m["drop"]; ok {
		t.Fatal("drop should be filtered")
	}
	want := []any{json.Number("1"), json.Number("2"), json.Number("3"), json.Number("4")}
	if !reflect.DeepEqual(m["keep"], want) {
		t.Fatalf("list order/values changed: %#v", m["keep"])
	}
}

func TestTopLevelPrimitive_PassThrough(t *testing.T) {
	for _, raw := range []string{`true`, `false`, `null`, `"s"`, `12345678901234567890`} {
		var out bytes.Buffer
		e := NewFieldFilteringEncoder(&out, []string{"x"})
		if err := e.EncodeReader(strings.NewReader(raw)); err != nil {
			t.Fatalf("EncodeReader: %v", err)
		}
		if out.String() != raw {
			t.Fatalf("primitive changed: got=%s want=%s", out.String(), raw)
		}
	}
}

func TestEncode_FromValue_MapFiltered_NoKeyOrderAssumption(t *testing.T) {
	in := map[string]any{"a": 1, "b": 2, "c": 3}
	var out bytes.Buffer
	e := NewFieldFilteringEncoder(&out, []string{"c", "a"})
	if err := e.Encode(in); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	m := decUseNumber(t, out.String()).(map[string]any)
	if len(m) != 2 {
		t.Fatalf("unexpected keys: %#v", m)
	}
	if m["a"].(json.Number).String() != "1" || m["c"].(json.Number).String() != "3" {
		t.Fatalf("values wrong: %#v", m)
	}
}
