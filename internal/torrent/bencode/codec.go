package bencode

import (
	"errors"
	"strconv"
	"strings"
)

var (
	ErrEmptyString   = errors.New("empty string")
	ErrInvalidString = errors.New("invalid bencoded string")
)

// Decode is now a thin wrapper around decodeElement + a trailing-data check.
func Decode(input string) (any, error) {
	if len(input) == 0 {
		return nil, ErrEmptyString
	}
	val, n, err := decodeElement(input)
	if err != nil {
		return nil, err
	}
	if n != len(input) {
		return nil, ErrInvalidString
	}
	return val, nil
}

// decodeString parses <length>:<data>.
func decodeString(str string) (string, int, error) {
	colon := strings.IndexByte(str, ':')
	if colon < 0 {
		return "", -1, ErrInvalidString
	}
	length, err := strconv.Atoi(str[:colon])
	if err != nil || length < 0 {
		return "", -1, ErrInvalidString
	}
	start, end := colon+1, colon+1+length
	if end > len(str) {
		return "", -1, ErrInvalidString
	}
	return str[start:end], end, nil
}

// decodeInt parses i<digits>e.
func decodeInt(str string) (int, int, error) {
	if len(str) < 3 || str[0] != 'i' {
		return 0, -1, ErrInvalidString
	}
	ePos := strings.IndexByte(str, 'e')
	if ePos < 0 {
		return 0, -1, ErrInvalidString
	}
	val, err := strconv.Atoi(str[1:ePos])
	if err != nil {
		return 0, -1, ErrInvalidString
	}
	return val, ePos + 1, nil
}

// decodeList parses l <elements> e, using decodeElement for each item.
func decodeList(str string) ([]any, int, error) {
	if len(str) == 0 || str[0] != 'l' {
		return nil, -1, ErrInvalidString
	}
	var (
		items []any
		i     = 1
	)
	for i < len(str) && str[i] != 'e' {
		elem, n, err := decodeElement(str[i:])
		if err != nil {
			return nil, -1, err
		}
		items = append(items, elem)
		i += n
	}
	if i >= len(str) || str[i] != 'e' {
		return nil, -1, ErrInvalidString
	}
	return items, i + 1, nil
}

// decodeDict parses d <key><value> … e, using decodeString for keys and decodeElement for values.
func decodeDict(str string) (map[string]any, int, error) {
	if len(str) == 0 || str[0] != 'd' {
		return nil, -1, ErrInvalidString
	}
	dict := make(map[string]any)
	var (
		i       = 1
		prevKey string
	)
	for i < len(str) && str[i] != 'e' {
		// decode key
		key, nKey, err := decodeString(str[i:])
		if err != nil {
			return nil, -1, err
		}
		if prevKey != "" && key <= prevKey {
			return nil, -1, ErrInvalidString
		}
		prevKey, i = key, i+nKey

		// decode value
		val, nVal, err := decodeElement(str[i:])
		if err != nil {
			return nil, -1, err
		}
		dict[key] = val
		i += nVal
	}
	if i >= len(str) || str[i] != 'e' {
		return nil, -1, ErrInvalidString
	}
	return dict, i + 1, nil
}

// decodeElement dispatches on the leading prefix and calls the appropriate decoder.
func decodeElement(str string) (any, int, error) {
	if len(str) == 0 {
		return nil, -1, ErrEmptyString
	}

	switch str[0] {
	case 'i':
		return decodeInt(str)
	case 'l':
		return decodeList(str)
	case 'd':
		return decodeDict(str)
	default:
		// must be a byte-string
		if str[0] < '0' || str[0] > '9' {
			return nil, -1, ErrInvalidString
		}
		return decodeString(str)
	}
}
