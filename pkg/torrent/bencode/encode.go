package bencode

import (
	"errors"
	"sort"
	"strconv"
)

var ErrEncoding = errors.New("error encoding data to bencode format")

func Encode(data any) ([]byte, error) {
	switch v := data.(type) {
	case string:
		bytes := []byte(v)
		return append([]byte(strconv.Itoa(len(bytes))+":"), bytes...), nil
	case []byte:
		return append([]byte(strconv.Itoa(len(v))+":"), v...), nil
	case int:
		return []byte("i" + strconv.FormatInt(int64(v), 10) + "e"), nil
	case int8:
		return []byte("i" + strconv.FormatInt(int64(v), 10) + "e"), nil
	case int16:
		return []byte("i" + strconv.FormatInt(int64(v), 10) + "e"), nil
	case int32:
		return []byte("i" + strconv.FormatInt(int64(v), 10) + "e"), nil
	case int64:
		return []byte("i" + strconv.FormatInt(v, 10) + "e"), nil
	case uint:
		return []byte("i" + strconv.FormatUint(uint64(v), 10) + "e"), nil
	case uint8:
		return []byte("i" + strconv.FormatUint(uint64(v), 10) + "e"), nil
	case uint16:
		return []byte("i" + strconv.FormatUint(uint64(v), 10) + "e"), nil
	case uint32:
		return []byte("i" + strconv.FormatUint(uint64(v), 10) + "e"), nil
	case uint64:
		return []byte("i" + strconv.FormatUint(v, 10) + "e"), nil
	case []any:
		var result []byte
		result = append(result, 'l')
		for _, item := range v {
			encoded, err := Encode(item)
			if err != nil {
				return nil, err
			}
			result = append(result, encoded...)
		}
		result = append(result, 'e')
		return result, nil
	case map[string]any:
		var result []byte
		result = append(result, 'd')

		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			keyEnc, err := Encode(k)
			if err != nil {
				return nil, err
			}
			result = append(result, keyEnc...)

			valEnc, err := Encode(v[k])
			if err != nil {
				return nil, err
			}
			result = append(result, valEnc...)
		}
		result = append(result, 'e')
		return result, nil
	default:
		return nil, ErrEncoding
	}
}
