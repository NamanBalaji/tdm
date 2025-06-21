package bencode

import (
	"errors"
	"strconv"
)

var ErrInvalidBencode = errors.New("invalid bencoded string")

func Decode(str []byte) (any, int, error) {
	if len(str) == 0 {
		return nil, 0, ErrInvalidBencode
	}

	switch {
	case '0' <= str[0] && str[0] <= '9':
		j := 0
		for j < len(str) && str[j] >= '0' && str[j] <= '9' {
			j++
		}
		if j == len(str) || str[j] != ':' {
			return nil, 0, ErrInvalidBencode
		}

		if j > 1 && str[0] == '0' {
			return nil, 0, ErrInvalidBencode
		}

		n, err := strconv.Atoi(string(str[:j]))
		if err != nil {
			return nil, 0, ErrInvalidBencode
		}
		start := j + 1
		end := start + n
		if end > len(str) {
			return nil, 0, ErrInvalidBencode
		}

		result := make([]byte, n)
		copy(result, str[start:end])
		return result, end, nil

	case str[0] == 'i':
		start := 1
		if start >= len(str) {
			return nil, 0, ErrInvalidBencode
		}

		if (str[start] == '0' && start+1 < len(str) && str[start+1] != 'e') ||
			(start+1 < len(str) && string(str[start:start+2]) == "-0") {
			return nil, 0, ErrInvalidBencode
		}
		j := start
		for j < len(str) && str[j] != 'e' {
			j++
		}
		if j == len(str) {
			return nil, 0, ErrInvalidBencode
		}
		n, err := strconv.ParseInt(string(str[start:j]), 10, 64)
		if err != nil {
			return nil, 0, ErrInvalidBencode
		}
		return n, j + 1, nil

	case str[0] == 'l':
		pos := 1
		var list []any
		for {
			if pos >= len(str) {
				return nil, 0, ErrInvalidBencode
			}
			if str[pos] == 'e' {
				return list, pos + 1, nil
			}
			item, n, err := Decode(str[pos:])
			if err != nil {
				return nil, 0, err
			}
			list = append(list, item)
			pos += n
		}
	case str[0] == 'd':
		decoded := make(map[string]any)
		pos := 1
		for {
			if pos >= len(str) {
				return nil, 0, ErrInvalidBencode
			}
			if str[pos] == 'e' {
				return decoded, pos + 1, nil
			}

			keyItem, n, err := Decode(str[pos:])
			if err != nil {
				return nil, 0, err
			}
			pos += n

			keyBytes, ok := keyItem.([]byte)
			if !ok {
				return nil, 0, ErrInvalidBencode
			}
			key := string(keyBytes)

			if pos >= len(str) {
				return nil, 0, ErrInvalidBencode
			}

			valItem, n, err := Decode(str[pos:])
			if err != nil {
				return nil, 0, err
			}
			pos += n

			if _, exists := decoded[key]; exists {
				return nil, 0, ErrInvalidBencode
			}

			decoded[key] = valItem
		}

	default:
		return nil, 0, ErrInvalidBencode
	}
}
