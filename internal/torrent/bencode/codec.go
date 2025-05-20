package bencode

import (
	"errors"
	"strconv"
)

var (
	ErrEmptyString   = errors.New("empty string")
	ErrInvalidString = errors.New("invalid bencoded string")
)

// Decode parses a complete bencoded input and returns the corresponding Go value.
func Decode(input string) (any, error) {
	if len(input) == 0 {
		return nil, ErrEmptyString
	}
	v, next, err := parseValue(input, 0)
	if err != nil {
		return nil, err
	}
	if next != len(input) {
		return nil, ErrInvalidString
	}
	return v, nil
}

// parseValue parses exactly one element starting at input[pos].
// It returns the parsed Go value (int, string, []any or map[string]any),
// the index immediately after the parsed element, or an error.
func parseValue(input string, pos int) (any, int, error) {
	if pos >= len(input) {
		return nil, -1, ErrInvalidString
	}
	switch input[pos] {
	case 'i':
		// integer: i<digits>e
		end := pos + 1
		for end < len(input) && input[end] != 'e' {
			end++
		}
		if end >= len(input) || input[end] != 'e' {
			return nil, -1, ErrInvalidString
		}
		n, err := strconv.Atoi(input[pos+1 : end])
		if err != nil {
			return nil, -1, ErrInvalidString
		}
		return n, end + 1, nil

	case 'l':
		// list: l <elements> e
		var list []any
		i := pos + 1
		for i < len(input) && input[i] != 'e' {
			elem, next, err := parseValue(input, i)
			if err != nil {
				return nil, -1, err
			}
			list = append(list, elem)
			i = next
		}
		if i >= len(input) || input[i] != 'e' {
			return nil, -1, ErrInvalidString
		}
		return list, i + 1, nil

	case 'd':
		// dict: d < key value pairs > e
		dict := make(map[string]any, 0)
		i := pos + 1
		var prevKey string
		for i < len(input) && input[i] != 'e' {
			keyVal, next, err := parseValue(input, i)
			if err != nil {
				return nil, -1, err
			}
			key, ok := keyVal.(string)
			if !ok {
				return nil, -1, ErrInvalidString
			}
			if prevKey != "" && key <= prevKey {
				return nil, -1, ErrInvalidString
			}
			prevKey = key
			i = next

			val, next2, err := parseValue(input, i)
			if err != nil {
				return nil, -1, err
			}
			dict[key] = val
			i = next2
		}
		if i >= len(input) || input[i] != 'e' {
			return nil, -1, ErrInvalidString
		}
		return dict, i + 1, nil

	default:
		// must be a byte-string: <length>:<data>
		end := pos
		for end < len(input) && input[end] >= '0' && input[end] <= '9' {
			end++
		}
		if end == pos || end >= len(input) || input[end] != ':' {
			return nil, -1, ErrInvalidString
		}
		length, err := strconv.Atoi(input[pos:end])
		if err != nil || length < 0 {
			return nil, -1, ErrInvalidString
		}
		start := end + 1
		finish := start + length
		if finish > len(input) {
			return nil, -1, ErrInvalidString
		}
		return input[start:finish], finish, nil
	}
}
