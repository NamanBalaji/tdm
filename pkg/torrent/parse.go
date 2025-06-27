package torrent

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"

	"github.com/NamanBalaji/tdm/pkg/torrent/bencode"
)

// ParseTorrent decodes the given bencoded data into a Metainfo struct.
// It also performs validation to ensure the torrent file is well-formed.
func ParseTorrent(data []byte) (*Metainfo, error) {
	if len(data) == 0 {
		return nil, newValidationError(ErrInvalidTorrentStructure, "data", "empty torrent data")
	}

	value, _, err := bencode.Decode(data)
	if err != nil {
		return nil, newValidationError(ErrInvalidTorrentStructure, "bencode",
			fmt.Sprintf("failed to decode bencode data: %v", err))
	}

	return parseMetainfo(value, data)
}

// parseMetainfo converts a bencode Value to a Metainfo struct.
func parseMetainfo(value any, originalData []byte) (*Metainfo, error) {
	dict, ok := value.(map[string]any)
	if !ok {
		return nil, newValidationError(ErrInvalidTorrentStructure, "root",
			fmt.Sprintf("torrent file must be a dictionary, got %T", value))
	}

	var metainfo Metainfo

	// Parse announce URL (required)
	if announceVal, exists := dict["announce"]; exists {
		if announceStr, err := bytesToString(announceVal); err == nil {
			metainfo.Announce = announceStr
		}
	} else {
		return nil, newValidationError(ErrInvalidAnnounceURL, "announce", "missing required announce field")
	}

	// Parse optional fields
	if announceListVal, exists := dict["announce-list"]; exists {
		if announceList, err := parseAnnounceList(announceListVal); err == nil {
			metainfo.AnnounceList = announceList
		}
	}

	if commentVal, exists := dict["comment"]; exists {
		if commentStr, err := bytesToString(commentVal); err == nil {
			metainfo.Comment = commentStr
		}
	}

	if createdByVal, exists := dict["created by"]; exists {
		if createdByStr, err := bytesToString(createdByVal); err == nil {
			metainfo.CreatedBy = createdByStr
		}
	}

	if encodingVal, exists := dict["encoding"]; exists {
		if encodingStr, err := bytesToString(encodingVal); err == nil {
			metainfo.Encoding = encodingStr
		}
	}

	if creationDateVal, exists := dict["creation date"]; exists {
		if creationDateInt, ok := creationDateVal.(int64); ok {
			metainfo.CreationDate = creationDateInt
		}
	}

	infoVal, exists := dict["info"]
	if !exists {
		return nil, newValidationError(ErrInvalidInfoDict, "info", "missing required info field")
	}

	info, err := parseInfo(infoVal)
	if err != nil {
		return nil, err
	}

	metainfo.Info = info

	// Efficiently extract the raw info dictionary bytes
	infoBytes, err := extractInfoBytes(originalData)
	if err != nil {
		return nil, newValidationError(ErrInvalidInfoDict, "info_bytes",
			fmt.Sprintf("failed to extract info bytes: %v", err))
	}

	metainfo.infoBytes = infoBytes

	err = metainfo.validate()
	if err != nil {
		return nil, err
	}

	return &metainfo, nil
}

// bytesToString safely converts []byte or string to string.
func bytesToString(value any) (string, error) {
	switch v := value.(type) {
	case []byte:
		return string(v), nil
	case string:
		return v, nil
	default:
		return "", fmt.Errorf("expected string or []byte, got %T", value)
	}
}

// parseAnnounceList parses the announce-list field.
func parseAnnounceList(value any) ([][]string, error) {
	list, ok := value.([]any)
	if !ok {
		return nil, newValidationError(ErrInvalidAnnounceList, "announce-list", "must be a list")
	}

	announceList := make([][]string, 0, len(list))
	for _, tierVal := range list {
		tierList, ok := tierVal.([]any)
		if !ok {
			continue // Skip malformed tier
		}

		tier := make([]string, 0, len(tierList))
		for _, urlVal := range tierList {
			if urlStr, err := bytesToString(urlVal); err == nil {
				tier = append(tier, urlStr)
			}
		}

		if len(tier) > 0 {
			announceList = append(announceList, tier)
		}
	}

	return announceList, nil
}

// parseInfo parses the info dictionary.
func parseInfo(value any) (Info, error) {
	infoDict, ok := value.(map[string]any)
	if !ok {
		return Info{}, newValidationError(ErrInvalidInfoDict, "info", "info must be a dictionary")
	}

	var info Info

	if nameVal, ok := infoDict["name"]; ok {
		info.Name, _ = bytesToString(nameVal)
	}

	if plVal, ok := infoDict["piece length"]; ok {
		info.PieceLength, _ = plVal.(int64)
	}

	if pVal, ok := infoDict["pieces"]; ok {
		info.Pieces, _ = bytesToString(pVal)
	}

	// **FIX**: Correctly parse both length and files if they both exist,
	// allowing the validation logic to catch the error.
	if lengthVal, exists := infoDict["length"]; exists {
		info.Length, _ = lengthVal.(int64)
		if md5Val, exists := infoDict["md5sum"]; exists {
			info.MD5Sum, _ = bytesToString(md5Val)
		}
	}

	if filesVal, exists := infoDict["files"]; exists {
		if files, err := parseFiles(filesVal); err == nil {
			info.Files = files
		}
	}

	return info, nil
}

// parseFiles parses the files array for multi-file torrents.
func parseFiles(value any) ([]File, error) {
	filesList, ok := value.([]any)
	if !ok {
		return nil, newValidationError(ErrInvalidMultiFile, "files", "files must be a list")
	}

	files := make([]File, 0, len(filesList))
	for _, fileVal := range filesList {
		fileDict, ok := fileVal.(map[string]any)
		if !ok {
			continue
		}

		var file File
		if lengthVal, ok := fileDict["length"].(int64); ok {
			file.Length = lengthVal
		}

		if pathVal, ok := fileDict["path"].([]any); ok {
			path := make([]string, 0, len(pathVal))
			for _, comp := range pathVal {
				if compStr, err := bytesToString(comp); err == nil {
					path = append(path, compStr)
				}
			}

			file.Path = path
		}

		if md5Val, exists := fileDict["md5sum"]; exists {
			file.MD5Sum, _ = bytesToString(md5Val)
		}

		files = append(files, file)
	}

	return files, nil
}

// extractInfoBytes finds the 'info' dictionary within the raw torrent data
// and returns its bencoded bytes without re-encoding.
func extractInfoBytes(data []byte) ([]byte, error) {
	// The info dictionary is prefixed by d...4:info
	infoKey := []byte("4:info")

	index := bytes.Index(data, infoKey)
	if index == -1 {
		return nil, errors.New("'info' dictionary not found")
	}

	// The 'd' for the main dictionary is before the index of '4:info'.
	// We need to find the start of the 'info' dictionary's value.
	startIndex := index + len(infoKey)
	if startIndex >= len(data) {
		return nil, errors.New("malformed torrent data after 'info' key")
	}

	// We need to parse the bencoded value that follows the key to find its end.
	_, endIndex, err := parseBencodedValue(data[startIndex:])
	if err != nil {
		return nil, fmt.Errorf("could not parse info dictionary value: %w", err)
	}

	return data[startIndex : startIndex+endIndex], nil
}

// parseBencodedValue is a helper to find the end of a bencoded element.
func parseBencodedValue(data []byte) (any, int, error) {
	if len(data) == 0 {
		return nil, 0, errors.New("cannot parse empty data")
	}

	switch {
	case data[0] >= '0' && data[0] <= '9': // String
		colon := bytes.IndexByte(data, ':')
		if colon == -1 {
			return nil, 0, errors.New("string length delimiter not found")
		}

		length, err := strconv.Atoi(string(data[:colon]))
		if err != nil {
			return nil, 0, err
		}

		end := colon + 1 + length
		if end > len(data) {
			return nil, 0, errors.New("string length exceeds data bounds")
		}

		return nil, end, nil

	case data[0] == 'i': // Integer
		end := bytes.IndexByte(data, 'e')
		if end == -1 {
			return nil, 0, errors.New("integer not terminated")
		}

		return nil, end + 1, nil

	case data[0] == 'l': // List
		pos := 1
		for pos < len(data) {
			if data[pos] == 'e' {
				return nil, pos + 1, nil
			}

			_, n, err := parseBencodedValue(data[pos:])
			if err != nil {
				return nil, 0, err
			}

			pos += n
		}

		return nil, 0, errors.New("list not terminated")

	case data[0] == 'd': // Dictionary
		pos := 1
		for pos < len(data) {
			if data[pos] == 'e' {
				return nil, pos + 1, nil
			}
			// Parse key
			_, n, err := parseBencodedValue(data[pos:])
			if err != nil {
				return nil, 0, err
			}

			pos += n
			// Parse value
			_, n, err = parseBencodedValue(data[pos:])
			if err != nil {
				return nil, 0, err
			}

			pos += n
		}

		return nil, 0, errors.New("dictionary not terminated")

	default:
		return nil, 0, fmt.Errorf("invalid bencode type prefix: %c", data[0])
	}
}
