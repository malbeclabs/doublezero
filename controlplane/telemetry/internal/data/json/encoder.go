package datajson

import (
	"bytes"
	"encoding/json"
	"io"
)

type Encoder interface{ Encode(v any) error }

type FieldFilteringEncoder struct {
	w      io.Writer
	fields map[string]struct{}
}

func NewFieldFilteringEncoder(w io.Writer, fields []string) *FieldFilteringEncoder {
	fs := make(map[string]struct{}, len(fields))
	for _, f := range fields {
		fs[f] = struct{}{}
	}
	return &FieldFilteringEncoder{w: w, fields: fs}
}

func (e *FieldFilteringEncoder) Encode(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	return e.encodeFromDecoder(dec)
}

func (e *FieldFilteringEncoder) EncodeReader(r io.Reader) error {
	dec := json.NewDecoder(r)
	dec.UseNumber()
	return e.encodeFromDecoder(dec)
}

func (e *FieldFilteringEncoder) encodeFromDecoder(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	switch d := tok.(type) {
	case json.Delim:
		switch d {
		case '{':
			return copyFilteredObject(dec, e.w, e.fields)
		case '[':
			if _, err := e.w.Write([]byte{'['}); err != nil {
				return err
			}
			first := true
			for dec.More() {
				if !first {
					if _, err := e.w.Write([]byte{','}); err != nil {
						return err
					}
				}
				first = false
				if err := copyFilteredValue(dec, e.w, e.fields); err != nil {
					return err
				}
			}
			if _, err := dec.Token(); err != nil {
				return err
			}
			_, err = e.w.Write([]byte{']'})
			return err
		default:
			_, err := e.w.Write([]byte(string(d)))
			return err
		}
	default:
		return writeToken(e.w, tok)
	}
}

func copyFilteredValue(dec *json.Decoder, w io.Writer, fields map[string]struct{}) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	switch d := tok.(type) {
	case json.Delim:
		switch d {
		case '{':
			return copyFilteredObject(dec, w, fields)
		case '[':
			if _, err := w.Write([]byte{'['}); err != nil {
				return err
			}
			first := true
			for dec.More() {
				if !first {
					if _, err := w.Write([]byte{','}); err != nil {
						return err
					}
				}
				first = false
				if err := copyFilteredValue(dec, w, fields); err != nil {
					return err
				}
			}
			if _, err := dec.Token(); err != nil {
				return err
			}
			_, err = w.Write([]byte{']'})
			return err
		default:
			_, err := w.Write([]byte(string(d)))
			return err
		}
	default:
		return writeToken(w, tok)
	}
}

func copyFilteredObject(dec *json.Decoder, w io.Writer, fields map[string]struct{}) error {
	if _, err := w.Write([]byte{'{'}); err != nil {
		return err
	}
	firstOut := true
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return err
		}
		k := kt.(string)
		_, allowed := fields[k]
		if !allowed {
			if err := skipValue(dec); err != nil {
				return err
			}
			continue
		}
		if !firstOut {
			if _, err := w.Write([]byte{','}); err != nil {
				return err
			}
		}
		firstOut = false
		if err := writeString(w, k); err != nil {
			return err
		}
		if _, err := w.Write([]byte{':'}); err != nil {
			return err
		}
		if err := copyFilteredValue(dec, w, fields); err != nil {
			return err
		}
	}
	if _, err := dec.Token(); err != nil {
		return err
	} // consume '}'
	_, err := w.Write([]byte{'}'})
	return err
}

func skipValue(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); ok {
		switch d {
		case '{':
			for dec.More() {
				if _, err := dec.Token(); err != nil {
					return err
				} // key
				if err := skipValue(dec); err != nil {
					return err
				} // value
			}
			_, err = dec.Token()
			return err
		case '[':
			for dec.More() {
				if err := skipValue(dec); err != nil {
					return err
				}
			}
			_, err = dec.Token()
			return err
		}
	}
	return nil
}

func writeString(w io.Writer, s string) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}

func writeToken(w io.Writer, tok any) error {
	switch v := tok.(type) {
	case string:
		return writeString(w, v)
	case json.Number:
		_, err := w.Write([]byte(v.String()))
		return err
	case bool:
		if v {
			_, err := w.Write([]byte("true"))
			return err
		}
		_, err := w.Write([]byte("false"))
		return err
	case nil:
		_, err := w.Write([]byte("null"))
		return err
	default:
		// fallback (shouldn't hit with UseNumber): re-marshal the token
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		_, err = w.Write(b)
		return err
	}
}
