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

func TestTopLevelObject_FilterOnlyTopLevel_NumbersAndLists(t *testing.T) {
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
	k0 := arr[0].(map[string]any)["k"].(json.Number)
	k1 := arr[1].(map[string]any)["k"].(json.Number)
	if k0.String() != "1" || k1.String() != "2" {
		t.Fatalf("array order changed: %#v", arr)
	}
}

func TestTopLevelArray_PassThroughOrder(t *testing.T) {
	raw := `[{"i":0},{"i":1},{"i":2},{"i":3}]`
	var out bytes.Buffer
	e := NewFieldFilteringEncoder(&out, []string{"unused"})
	if err := e.EncodeReader(strings.NewReader(raw)); err != nil {
		t.Fatalf("EncodeReader: %v", err)
	}
	if out.String() != raw {
		t.Fatalf("array should pass through unchanged; got=%s", out.String())
	}

	raw2 := `{"keep":[1,2,3,4],"drop":[9,9]}`
	out.Reset()
	e = NewFieldFilteringEncoder(&out, []string{"keep"})
	if err := e.EncodeReader(strings.NewReader(raw2)); err != nil {
		t.Fatalf("EncodeReader: %v", err)
	}
	v := decUseNumber(t, out.String()).(map[string]any)
	if _, ok := v["drop"]; ok {
		t.Fatal("drop should be filtered")
	}
	want := []any{json.Number("1"), json.Number("2"), json.Number("3"), json.Number("4")}
	if !reflect.DeepEqual(v["keep"], want) {
		t.Fatalf("list order/values changed: %#v", v["keep"])
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
