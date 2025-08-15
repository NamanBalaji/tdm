package bencode

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"strconv"
)

// Unmarshal parses bencoded data into v, which must be a non-nil pointer.
func Unmarshal(data []byte, v any) error {
	if v == nil {
		return errors.New("bencode: Unmarshal(nil)")
	}

	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return errors.New("bencode: Unmarshal target must be non-nil pointer")
	}

	val, n, err := parseValue(data)
	if err != nil {
		return err
	}

	if n != len(data) {
		return errors.New("bencode: trailing data after top value")
	}

	return assign(val, rv.Elem())
}

// parseValue reads the next bencoded value from src. It returns the
// decoded value, the number of bytes consumed and an error if
// decoding fails.
func parseValue(src []byte) (any, int, error) {
	if len(src) == 0 {
		return nil, 0, errors.New("bencode: unexpected EOF")
	}

	switch src[0] {
	case 'i':
		return parseInt(src)
	case 'l':
		return parseList(src)
	case 'd':
		return parseDict(src)
	default:
		if src[0] < '0' || src[0] > '9' {
			return nil, 0, fmt.Errorf("bencode: invalid prefix 0x%02x", src[0])
		}

		return parseString(src)
	}
}

// parseInt parses an integer from src. The format is i<number>e.
func parseInt(src []byte) (int64, int, error) {
	idx := bytes.IndexByte(src, 'e')
	if idx == -1 {
		return 0, 0, errors.New("bencode: unterminated int")
	}

	s := string(src[1:idx])
	if len(s) == 0 {
		return 0, 0, errors.New("bencode: empty int")
	}
	// Disallow leading zeros.
	if (s[0] == '-' && len(s) > 1 && s[1] == '0') || (s[0] == '0' && len(s) > 1) {
		return 0, 0, errors.New("bencode: leading zeros in int")
	}

	val, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, 0, err
	}

	return val, idx + 1, nil
}

// parseString parses a length-prefixed string from src.
func parseString(src []byte) (string, int, error) {
	col := bytes.IndexByte(src, ':')
	if col == -1 {
		return "", 0, errors.New("bencode: missing ':' in string")
	}

	length64, err := strconv.ParseInt(string(src[:col]), 10, 64)
	if err != nil || length64 < 0 {
		return "", 0, errors.New("bencode: invalid string length")
	}

	length := int(length64)

	start := col + 1
	if start+length > len(src) {
		return "", 0, errors.New("bencode: string exceeds input length")
	}

	return string(src[start : start+length]), start + length, nil
}

// parseList parses a list of values from src. The format is
// l<item1><item2>...e.
func parseList(src []byte) ([]any, int, error) {
	pos := 1

	var list []any

	for {
		if pos >= len(src) {
			return nil, 0, errors.New("bencode: unterminated list")
		}

		if src[pos] == 'e' {
			return list, pos + 1, nil
		}

		val, n, err := parseValue(src[pos:])
		if err != nil {
			return nil, 0, err
		}

		list = append(list, val)
		pos += n
	}
}

// parseDict parses a dictionary from src. The format is
// d<string><value>...e. Keys must appear in lexicographic order.
func parseDict(src []byte) (map[string]any, int, error) {
	pos := 1
	dict := make(map[string]any)

	var lastKey string

	for {
		if pos >= len(src) {
			return nil, 0, errors.New("bencode: unterminated dict")
		}

		if src[pos] == 'e' {
			return dict, pos + 1, nil
		}

		key, n, err := parseString(src[pos:])
		if err != nil {
			return nil, 0, err
		}
		// Keys must be in sorted order according to the spec.
		if lastKey >= key {
			return nil, 0, errors.New("bencode: dict keys not in order")
		}

		lastKey = key
		pos += n

		val, m, err := parseValue(src[pos:])
		if err != nil {
			return nil, 0, err
		}

		dict[key] = val
		pos += m
	}
}

// assign copies src into dst. It performs type assertions and
// conversions to populate dst with the decoded bencoded value.
func assign(src any, dst reflect.Value) error {
	if !dst.CanSet() {
		return errors.New("bencode: cannot set destination")
	}

	switch dst.Kind() {
	case reflect.String:
		s, ok := src.(string)
		if !ok {
			return typeErr(dst.Type(), src)
		}

		dst.SetString(s)

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		switch v := src.(type) {
		case int64:
			dst.SetInt(v)
		case string:
			i, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return typeErr(dst.Type(), src)
			}

			dst.SetInt(i)
		default:
			return typeErr(dst.Type(), src)
		}

	case reflect.Slice:
		if dst.Type().Elem().Kind() == reflect.Uint8 {
			s, ok := src.(string)
			if !ok {
				return typeErr(dst.Type(), src)
			}

			dst.SetBytes([]byte(s))

			return nil
		}

		arr, ok := src.([]any)
		if !ok {
			return typeErr(dst.Type(), src)
		}

		slice := reflect.MakeSlice(dst.Type(), len(arr), len(arr))
		for i, e := range arr {
			if err := assign(e, slice.Index(i)); err != nil {
				return err
			}
		}

		dst.Set(slice)

	case reflect.Map:
		d, ok := src.(map[string]any)
		if !ok {
			return typeErr(dst.Type(), src)
		}

		if dst.IsNil() {
			dst.Set(reflect.MakeMap(dst.Type()))
		}

		for k, v := range d {
			keyVal := reflect.ValueOf(k).Convert(dst.Type().Key())

			valVal := reflect.New(dst.Type().Elem()).Elem()
			if err := assign(v, valVal); err != nil {
				return err
			}

			dst.SetMapIndex(keyVal, valVal)
		}

	case reflect.Struct:
		d, ok := src.(map[string]any)
		if !ok {
			return typeErr(dst.Type(), src)
		}

		t := dst.Type()
		fieldMap := make(map[string]int)

		for i := 0; i < t.NumField(); i++ {
			sf := t.Field(i)
			if sf.PkgPath != "" {
				continue
			}

			key := sf.Tag.Get("bencode")
			if key == "" {
				key = sf.Name
			}

			fieldMap[key] = i
		}

		for k, v := range d {
			if idx, ok := fieldMap[k]; ok {
				if err := assign(v, dst.Field(idx)); err != nil {
					return err
				}
			}
		}

	default:
		return fmt.Errorf("bencode: unsupported destination %s", dst.Type())
	}

	return nil
}

// typeErr returns a formatted error for a type mismatch.
func typeErr(dst reflect.Type, src any) error {
	return fmt.Errorf("bencode: cannot assign %T to %s", src, dst)
}
