package bencode

import "strings"

func DecodeString(str string) string {
	return strings.Split(str, ":")[1]
}
