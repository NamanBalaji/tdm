package bencode

import (
	"bytes"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strconv"
)

// Marshal returns the bencode encoding of v.
func Marshal(v interface{}) ([]byte, error) {
	var buf bytes.Buffer
	if err := NewEncoder(&buf).Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Encoder encodes values to bencode format.
type Encoder struct {
	w io.Writer
}

// NewEncoder creates a new encoder.
func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: w}
}

// Encode writes the bencode encoding of v to the stream.
func (e *Encoder) Encode(v interface{}) error {
	return e.encodeValue(reflect.ValueOf(v))
}

// encodeValue encodes a reflect.Value.
func (e *Encoder) encodeValue(v reflect.Value) error {
	// Handle nil pointers and interfaces
	if !v.IsValid() || (v.Kind() == reflect.Ptr && v.IsNil()) {
		return e.encodeBytes(nil)
	}

	// Dereference pointers and interfaces
	for v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return e.encodeBytes(nil)
		}
		v = v.Elem()
	}

	// Handle custom marshaler
	if marshaler, ok := v.Interface().(Marshaler); ok {
		data, err := marshaler.MarshalBencode()
		if err != nil {
			return err
		}
		_, err = e.w.Write(data)
		return err
	}

	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return e.encodeInt(v.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return e.encodeInt(int64(v.Uint()))
	case reflect.String:
		return e.encodeString(v.String())
	case reflect.Slice, reflect.Array:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			// []byte or [N]byte
			return e.encodeBytes(v.Bytes())
		}
		return e.encodeList(v)
	case reflect.Map:
		return e.encodeMap(v)
	case reflect.Struct:
		return e.encodeStruct(v)
	default:
		return fmt.Errorf("bencode: unsupported type %v", v.Type())
	}
}

// encodeInt encodes an integer.
func (e *Encoder) encodeInt(i int64) error {
	_, err := fmt.Fprintf(e.w, "i%de", i)
	return err
}

// encodeString encodes a string.
func (e *Encoder) encodeString(s string) error {
	return e.encodeBytes([]byte(s))
}

// encodeBytes encodes a byte slice.
func (e *Encoder) encodeBytes(b []byte) error {
	if b == nil {
		_, err := e.w.Write([]byte("0:"))
		return err
	}
	_, err := fmt.Fprintf(e.w, "%d:", len(b))
	if err != nil {
		return err
	}
	_, err = e.w.Write(b)
	return err
}

// encodeList encodes a slice or array.
func (e *Encoder) encodeList(v reflect.Value) error {
	if _, err := e.w.Write([]byte("l")); err != nil {
		return err
	}

	for i := 0; i < v.Len(); i++ {
		if err := e.encodeValue(v.Index(i)); err != nil {
			return err
		}
	}

	_, err := e.w.Write([]byte("e"))
	return err
}

// encodeMap encodes a map.
func (e *Encoder) encodeMap(v reflect.Value) error {
	if v.Type().Key().Kind() != reflect.String {
		return fmt.Errorf("bencode: map key must be string, got %v", v.Type().Key())
	}

	// Get and sort keys
	keys := v.MapKeys()
	strKeys := make([]string, len(keys))
	for i, k := range keys {
		strKeys[i] = k.String()
	}
	sort.Strings(strKeys)

	if _, err := e.w.Write([]byte("d")); err != nil {
		return err
	}

	for _, k := range strKeys {
		// Encode key
		if err := e.encodeString(k); err != nil {
			return err
		}

		// Encode value
		if err := e.encodeValue(v.MapIndex(reflect.ValueOf(k))); err != nil {
			return err
		}
	}

	_, err := e.w.Write([]byte("e"))
	return err
}

// encodeStruct encodes a struct as a dictionary.
func (e *Encoder) encodeStruct(v reflect.Value) error {
	// Collect fields
	type field struct {
		key   string
		value reflect.Value
	}

	var fields []field
	t := v.Type()

	for i := 0; i < v.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" { // unexported
			continue
		}

		fv := v.Field(i)

		// Skip zero values if omitempty
		tag := f.Tag.Get("bencode")
		if tag == "-" {
			continue
		}

		omitempty := false
		if idx := bytes.IndexByte([]byte(tag), ','); idx != -1 {
			if tag[idx+1:] == "omitempty" {
				omitempty = true
			}
			tag = tag[:idx]
		}

		if omitempty && isZero(fv) {
			continue
		}

		// Get field name
		key := tag
		if key == "" {
			key = f.Name
			// Convert to bencode format (lowercase first letter)
			if len(key) > 0 {
				key = string(bytes.ToLower([]byte{key[0]})) + key[1:]
			}
		}

		fields = append(fields, field{key: key, value: fv})
	}

	// Sort fields by key
	sort.Slice(fields, func(i, j int) bool {
		return fields[i].key < fields[j].key
	})

	// Encode as dictionary
	if _, err := e.w.Write([]byte("d")); err != nil {
		return err
	}

	for _, f := range fields {
		if err := e.encodeString(f.key); err != nil {
			return err
		}
		if err := e.encodeValue(f.value); err != nil {
			return err
		}
	}

	_, err := e.w.Write([]byte("e"))
	return err
}

// isZero reports whether v is the zero value for its type.
func isZero(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.String:
		return v.String() == ""
	case reflect.Array, reflect.Map, reflect.Slice:
		return v.Len() == 0
	case reflect.Interface, reflect.Ptr:
		return v.IsNil()
	case reflect.Struct:
		// Check if all fields are zero
		for i := 0; i < v.NumField(); i++ {
			if !isZero(v.Field(i)) {
				return false
			}
		}
		return true
	}
	return false
}

// Marshaler is the interface implemented by types that can marshal themselves to bencode.
type Marshaler interface {
	MarshalBencode() ([]byte, error)
}

// EncodeString encodes a string to bencode format.
func EncodeString(s string) []byte {
	return append([]byte(strconv.Itoa(len(s))+":"), s...)
}

// EncodeInt encodes an integer to bencode format.
func EncodeInt(i int64) []byte {
	return []byte(fmt.Sprintf("i%de", i))
}
