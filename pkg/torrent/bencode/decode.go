package bencode

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// Decoder errors.
var (
	ErrInvalidBencode = errors.New("invalid bencode")
	ErrUnexpectedEOF  = errors.New("unexpected EOF")
	ErrInvalidType    = errors.New("invalid type for bencode")
)

// Unmarshal decodes bencode data into v.
func Unmarshal(data []byte, v interface{}) error {
	r := bufio.NewReader(strings.NewReader(string(data)))
	return NewDecoder(r).Decode(v)
}

// Decoder decodes bencode data.
type Decoder struct {
	r *bufio.Reader
}

// NewDecoder creates a new decoder.
func NewDecoder(r io.Reader) *Decoder {
	br, ok := r.(*bufio.Reader)
	if !ok {
		br = bufio.NewReader(r)
	}
	return &Decoder{r: br}
}

// Decode decodes bencode data into v.
func (d *Decoder) Decode(v interface{}) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return errors.New("bencode: Decode requires non-nil pointer")
	}

	val, err := d.decodeValue()
	if err != nil {
		return err
	}

	return d.unmarshalValue(val, rv.Elem())
}

// decodeValue decodes a single bencode value.
func (d *Decoder) decodeValue() (interface{}, error) {
	b, err := d.r.Peek(1)
	if err != nil {
		return nil, err
	}

	switch b[0] {
	case 'i':
		return d.decodeInt()
	case 'l':
		return d.decodeList()
	case 'd':
		return d.decodeDict()
	case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return d.decodeString()
	default:
		return nil, fmt.Errorf("%w: unexpected character %c", ErrInvalidBencode, b[0])
	}
}

// decodeInt decodes a bencode integer.
func (d *Decoder) decodeInt() (int64, error) {
	// Read 'i'
	if _, err := d.r.ReadByte(); err != nil {
		return 0, err
	}

	// Read number until 'e'
	var numStr []byte
	for {
		b, err := d.r.ReadByte()
		if err != nil {
			return 0, err
		}
		if b == 'e' {
			break
		}
		numStr = append(numStr, b)
	}

	if len(numStr) == 0 {
		return 0, fmt.Errorf("%w: empty integer", ErrInvalidBencode)
	}

	// Check for invalid formats like i-0e or i03e
	if len(numStr) > 1 && numStr[0] == '0' {
		return 0, fmt.Errorf("%w: leading zeros in integer", ErrInvalidBencode)
	}
	if len(numStr) > 1 && numStr[0] == '-' && numStr[1] == '0' {
		return 0, fmt.Errorf("%w: negative zero", ErrInvalidBencode)
	}

	return strconv.ParseInt(string(numStr), 10, 64)
}

// decodeString decodes a bencode string.
func (d *Decoder) decodeString() ([]byte, error) {
	// Read length
	var lenStr []byte
	for {
		b, err := d.r.ReadByte()
		if err != nil {
			return nil, err
		}
		if b == ':' {
			break
		}
		if b < '0' || b > '9' {
			return nil, fmt.Errorf("%w: invalid string length", ErrInvalidBencode)
		}
		lenStr = append(lenStr, b)
	}

	length, err := strconv.ParseInt(string(lenStr), 10, 64)
	if err != nil {
		return nil, err
	}

	if length < 0 {
		return nil, fmt.Errorf("%w: negative string length", ErrInvalidBencode)
	}

	// Read string data
	data := make([]byte, length)
	n, err := io.ReadFull(d.r, data)
	if err != nil {
		return nil, err
	}
	if int64(n) != length {
		return nil, ErrUnexpectedEOF
	}

	return data, nil
}

// decodeList decodes a bencode list.
func (d *Decoder) decodeList() ([]interface{}, error) {
	// Read 'l'
	if _, err := d.r.ReadByte(); err != nil {
		return nil, err
	}

	var list []interface{}
	for {
		b, err := d.r.Peek(1)
		if err != nil {
			return nil, err
		}

		if b[0] == 'e' {
			d.r.ReadByte() // consume 'e'
			break
		}

		val, err := d.decodeValue()
		if err != nil {
			return nil, err
		}

		list = append(list, val)
	}

	return list, nil
}

// decodeDict decodes a bencode dictionary.
func (d *Decoder) decodeDict() (map[string]interface{}, error) {
	// Read 'd'
	if _, err := d.r.ReadByte(); err != nil {
		return nil, err
	}

	dict := make(map[string]interface{})
	var lastKey string

	for {
		b, err := d.r.Peek(1)
		if err != nil {
			return nil, err
		}

		if b[0] == 'e' {
			d.r.ReadByte() // consume 'e'
			break
		}

		// Decode key (must be string)
		keyBytes, err := d.decodeString()
		if err != nil {
			return nil, fmt.Errorf("decoding dict key: %w", err)
		}
		key := string(keyBytes)

		// Check that keys are sorted
		if lastKey != "" && key <= lastKey {
			return nil, fmt.Errorf("%w: dictionary keys not sorted", ErrInvalidBencode)
		}
		lastKey = key

		// Decode value
		val, err := d.decodeValue()
		if err != nil {
			return nil, fmt.Errorf("decoding dict value for key %q: %w", key, err)
		}

		dict[key] = val
	}

	return dict, nil
}

// unmarshalValue assigns a decoded value to a reflect.Value.
func (d *Decoder) unmarshalValue(val interface{}, rv reflect.Value) error {
	// Handle pointers
	if rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			rv.Set(reflect.New(rv.Type().Elem()))
		}
		return d.unmarshalValue(val, rv.Elem())
	}

	switch v := val.(type) {
	case int64:
		return d.unmarshalInt(v, rv)
	case []byte:
		return d.unmarshalBytes(v, rv)
	case []interface{}:
		return d.unmarshalList(v, rv)
	case map[string]interface{}:
		return d.unmarshalDict(v, rv)
	default:
		return fmt.Errorf("%w: cannot unmarshal %T into %v", ErrInvalidType, val, rv.Type())
	}
}

// unmarshalInt assigns an int64 to appropriate types.
func (d *Decoder) unmarshalInt(val int64, rv reflect.Value) error {
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		rv.SetInt(val)
		return nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if val < 0 {
			return fmt.Errorf("%w: cannot unmarshal negative int to uint", ErrInvalidType)
		}
		rv.SetUint(uint64(val))
		return nil
	case reflect.Interface:
		rv.Set(reflect.ValueOf(val))
		return nil
	default:
		return fmt.Errorf("%w: cannot unmarshal int into %v", ErrInvalidType, rv.Type())
	}
}

// unmarshalBytes assigns []byte to appropriate types.
func (d *Decoder) unmarshalBytes(val []byte, rv reflect.Value) error {
	switch rv.Kind() {
	case reflect.String:
		rv.SetString(string(val))
		return nil
	case reflect.Slice:
		if rv.Type().Elem().Kind() == reflect.Uint8 {
			rv.SetBytes(val)
			return nil
		}
		return fmt.Errorf("%w: cannot unmarshal bytes into %v", ErrInvalidType, rv.Type())
	case reflect.Array:
		if rv.Type().Elem().Kind() == reflect.Uint8 {
			reflect.Copy(rv, reflect.ValueOf(val))
			return nil
		}
		return fmt.Errorf("%w: cannot unmarshal bytes into %v", ErrInvalidType, rv.Type())
	case reflect.Interface:
		rv.Set(reflect.ValueOf(val))
		return nil
	default:
		return fmt.Errorf("%w: cannot unmarshal bytes into %v", ErrInvalidType, rv.Type())
	}
}

// unmarshalList assigns []interface{} to appropriate types.
func (d *Decoder) unmarshalList(val []interface{}, rv reflect.Value) error {
	switch rv.Kind() {
	case reflect.Slice:
		slice := reflect.MakeSlice(rv.Type(), len(val), len(val))
		for i, v := range val {
			if err := d.unmarshalValue(v, slice.Index(i)); err != nil {
				return err
			}
		}
		rv.Set(slice)
		return nil
	case reflect.Interface:
		rv.Set(reflect.ValueOf(val))
		return nil
	default:
		return fmt.Errorf("%w: cannot unmarshal list into %v", ErrInvalidType, rv.Type())
	}
}

// unmarshalDict assigns map[string]interface{} to appropriate types.
func (d *Decoder) unmarshalDict(val map[string]interface{}, rv reflect.Value) error {
	switch rv.Kind() {
	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String {
			return fmt.Errorf("%w: map key must be string, got %v", ErrInvalidType, rv.Type().Key())
		}

		mapVal := reflect.MakeMap(rv.Type())
		for k, v := range val {
			keyVal := reflect.ValueOf(k)
			elemVal := reflect.New(rv.Type().Elem()).Elem()
			if err := d.unmarshalValue(v, elemVal); err != nil {
				return err
			}
			mapVal.SetMapIndex(keyVal, elemVal)
		}
		rv.Set(mapVal)
		return nil

	case reflect.Struct:
		return d.unmarshalStruct(val, rv)

	case reflect.Interface:
		rv.Set(reflect.ValueOf(val))
		return nil

	default:
		return fmt.Errorf("%w: cannot unmarshal dict into %v", ErrInvalidType, rv.Type())
	}
}

// unmarshalStruct assigns map values to struct fields.
func (d *Decoder) unmarshalStruct(val map[string]interface{}, rv reflect.Value) error {
	rt := rv.Type()

	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		if field.PkgPath != "" { // unexported
			continue
		}

		// Get field name from tag or use field name
		key := field.Name
		if tag := field.Tag.Get("bencode"); tag != "" && tag != "-" {
			key = tag
		}

		// Convert field name to bencode format (lowercase first letter)
		if key == field.Name && len(key) > 0 {
			key = strings.ToLower(key[:1]) + key[1:]
		}

		if v, ok := val[key]; ok {
			if err := d.unmarshalValue(v, rv.Field(i)); err != nil {
				return fmt.Errorf("field %s: %w", field.Name, err)
			}
		}
	}

	return nil
}

// RawMessage is a raw encoded bencode value.
type RawMessage []byte

// UnmarshalBencode implements custom unmarshaling.
func (m *RawMessage) UnmarshalBencode(data []byte) error {
	*m = append((*m)[0:0], data...)
	return nil
}

// OrderedKeys returns the keys of a bencode dictionary in sorted order.
func OrderedKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
