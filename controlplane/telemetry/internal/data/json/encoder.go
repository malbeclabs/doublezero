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
			if _, err := e.w.Write([]byte{'{'}); err != nil {
				return err
			}
			first := true
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return err
				}
				k := kt.(string)
				if _, ok := e.fields[k]; ok {
					if !first {
						if _, err := e.w.Write([]byte{','}); err != nil {
							return err
						}
					}
					first = false
					if err := writeString(e.w, k); err != nil {
						return err
					}
					if _, err := e.w.Write([]byte{':'}); err != nil {
						return err
					}
					if err := copyValue(dec, e.w); err != nil {
						return err
					}
				} else {
					if err := skipValue(dec); err != nil {
						return err
					}
				}
			}
			_, err = dec.Token()
			if err != nil {
				return err
			}
			_, err = e.w.Write([]byte{'}'})
			return err
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
				if err := copyValue(dec, e.w); err != nil {
					return err
				}
			}
			_, err = dec.Token()
			if err != nil {
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

func copyValue(dec *json.Decoder, w io.Writer) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	switch d := tok.(type) {
	case json.Delim:
		switch d {
		case '{':
			if _, err := w.Write([]byte{'{'}); err != nil {
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
				kt, err := dec.Token()
				if err != nil {
					return err
				}
				if err := writeString(w, kt.(string)); err != nil {
					return err
				}
				if _, err := w.Write([]byte{':'}); err != nil {
					return err
				}
				if err := copyValue(dec, w); err != nil {
					return err
				}
			}
			_, err = dec.Token()
			if err != nil {
				return err
			}
			_, err = w.Write([]byte{'}'})
			return err
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
				if err := copyValue(dec, w); err != nil {
					return err
				}
			}
			_, err = dec.Token()
			if err != nil {
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

func skipValue(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); ok {
		switch d {
		case '{':
			for dec.More() {
				if err := skipValue(dec); err != nil {
					return err
				}
			}
			_, err = dec.Token()
		case '[':
			for dec.More() {
				if err := skipValue(dec); err != nil {
					return err
				}
			}
			_, err = dec.Token()
		}
	}
	return err
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
