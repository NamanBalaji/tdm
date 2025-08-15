package bencode

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
)

// Marshal encodes v into canonical bencode.
func Marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	if err := encodeValue(&buf, reflect.ValueOf(v)); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// encodeValue walks the value v and writes its bencoded
// representation to buf. Only types supported by the BitTorrent
// specification (strings, integers, lists and dictionaries) are
// allowed. Unsupported types will return an error.
func encodeValue(buf *bytes.Buffer, v reflect.Value) error {
	if !v.IsValid() {
		buf.WriteString("0:")
		return nil
	}

	if v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			buf.WriteString("0:")

			return nil
		}

		return encodeValue(buf, v.Elem())
	}

	switch v.Kind() {
	case reflect.String:
		s := v.String()
		buf.WriteString(strconv.Itoa(len(s)))
		buf.WriteByte(':')
		buf.WriteString(s)

	case reflect.Slice, reflect.Array:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			s := v.Bytes()
			buf.WriteString(strconv.Itoa(len(s)))
			buf.WriteByte(':')
			buf.Write(s)

			return nil
		}

		buf.WriteByte('l')

		for i := 0; i < v.Len(); i++ {
			if err := encodeValue(buf, v.Index(i)); err != nil {
				return err
			}
		}

		buf.WriteByte('e')

	case reflect.Map:
		if v.Type().Key().Kind() != reflect.String {
			return errors.New("bencode: map keys must be strings")
		}

		buf.WriteByte('d')

		keys := make([]string, 0, v.Len())
		for _, k := range v.MapKeys() {
			keys = append(keys, k.String())
		}

		sort.Strings(keys)

		for _, k := range keys {
			if err := encodeValue(buf, reflect.ValueOf(k)); err != nil {
				return err
			}

			if err := encodeValue(buf, v.MapIndex(reflect.ValueOf(k))); err != nil {
				return err
			}
		}

		buf.WriteByte('e')

	case reflect.Struct:
		tmp := make(map[string]any, v.NumField())

		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			sf := t.Field(i)
			if sf.PkgPath != "" {
				continue
			}

			tag := sf.Tag.Get("bencode")
			if tag == "-" {
				continue
			}

			key := tag
			if key == "" {
				key = sf.Name
			}

			tmp[key] = v.Field(i).Interface()
		}

		return encodeValue(buf, reflect.ValueOf(tmp))

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		buf.WriteByte('i')
		buf.WriteString(strconv.FormatInt(v.Int(), 10))
		buf.WriteByte('e')

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		buf.WriteByte('i')
		buf.WriteString(strconv.FormatUint(v.Uint(), 10))
		buf.WriteByte('e')

	case reflect.Bool:
		if v.Bool() {
			buf.WriteString("i1e")
		} else {
			buf.WriteString("i0e")
		}

	default:
		return fmt.Errorf("bencode: unsupported type %s", v.Type())
	}

	return nil
}
