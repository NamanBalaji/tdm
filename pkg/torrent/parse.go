package torrent

import (
	"errors"
	"fmt"

	"github.com/NamanBalaji/tdm/pkg/torrent/bencode"
)

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
		announceStr, err := bytesToString(announceVal)
		if err != nil {
			return nil, newValidationError(ErrInvalidAnnounceURL, "announce",
				fmt.Sprintf("announce field must be a string, got %T", announceVal))
		}
		metainfo.Announce = announceStr
	} else {
		return nil, newValidationError(ErrInvalidAnnounceURL, "announce", "missing required announce field")
	}

	// Parse announce-list (optional)
	if announceListVal, exists := dict["announce-list"]; exists {
		announceList, err := parseAnnounceList(announceListVal)
		if err != nil {
			return nil, err // Error already wrapped in parseAnnounceList
		}
		metainfo.AnnounceList = announceList
	}

	// Parse optional string fields
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

	info, infoBytes, err := parseInfo(infoVal, originalData)
	if err != nil {
		return nil, err
	}
	metainfo.Info = info
	metainfo.setInfoBytes(infoBytes)

	if err := metainfo.validate(); err != nil {
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
		return nil, newValidationError(ErrInvalidAnnounceList, "announce-list",
			fmt.Sprintf("announce-list must be a list, got %T", value))
	}

	announceList := make([][]string, 0, len(list))

	for i, tierVal := range list {
		tierList, ok := tierVal.([]any)
		if !ok {
			return nil, newValidationError(ErrInvalidAnnounceList, "announce-list",
				fmt.Sprintf("announce-list[%d] must be a list, got %T", i, tierVal))
		}

		tier := make([]string, 0, len(tierList))
		for j, urlVal := range tierList {
			urlStr, err := bytesToString(urlVal)
			if err != nil {
				return nil, newValidationError(ErrInvalidAnnounceList, "announce-list",
					fmt.Sprintf("announce-list[%d][%d] must be a string, got %T", i, j, urlVal))
			}
			tier = append(tier, urlStr)
		}

		if len(tier) > 0 {
			announceList = append(announceList, tier)
		}
	}

	return announceList, nil
}

// parseInfo parses the info dictionary and extracts its raw bytes for hash calculation.
func parseInfo(value any, originalData []byte) (Info, []byte, error) {
	infoDict, ok := value.(map[string]any)
	if !ok {
		return Info{}, nil, newValidationError(ErrInvalidInfoDict, "info",
			fmt.Sprintf("info must be a dictionary, got %T", value))
	}

	var info Info

	nameVal, exists := infoDict["name"]
	if !exists {
		return Info{}, nil, newValidationError(ErrInvalidName, "name", "missing required name field")
	}
	nameStr, err := bytesToString(nameVal)
	if err != nil {
		return Info{}, nil, newValidationError(ErrInvalidName, "name",
			fmt.Sprintf("name must be a string, got %T", nameVal))
	}
	info.Name = nameStr

	pieceLengthVal, exists := infoDict["piece length"]
	if !exists {
		return Info{}, nil, newValidationError(ErrInvalidPieceLength, "piece length",
			"missing required piece length field")
	}
	pieceLengthInt, ok := pieceLengthVal.(int64)
	if !ok {
		return Info{}, nil, newValidationError(ErrInvalidPieceLength, "piece length",
			fmt.Sprintf("piece length must be an integer, got %T", pieceLengthVal))
	}
	info.PieceLength = pieceLengthInt

	piecesVal, exists := infoDict["pieces"]
	if !exists {
		return Info{}, nil, newValidationError(ErrInvalidPieces, "pieces", "missing required pieces field")
	}

	var piecesBytes []byte
	switch v := piecesVal.(type) {
	case []byte:
		piecesBytes = v
	case string:
		piecesBytes = []byte(v)
	default:
		return Info{}, nil, newValidationError(ErrInvalidPieces, "pieces",
			fmt.Sprintf("pieces must be binary data, got %T", piecesVal))
	}
	info.Pieces = string(piecesBytes)

	if len(info.Pieces)%20 != 0 {
		return Info{}, nil, newValidationError(ErrInvalidPieces, "pieces",
			fmt.Sprintf("pieces length must be multiple of 20, got %d", len(info.Pieces)))
	}

	if lengthVal, exists := infoDict["length"]; exists {
		lengthInt, ok := lengthVal.(int64)
		if !ok {
			return Info{}, nil, newValidationError(ErrInvalidSingleFile, "length",
				fmt.Sprintf("length must be an integer, got %T", lengthVal))
		}
		info.Length = lengthInt

		if md5Val, exists := infoDict["md5sum"]; exists {
			if md5Str, err := bytesToString(md5Val); err == nil {
				info.MD5Sum = md5Str
			}
		}
	}

	if filesVal, exists := infoDict["files"]; exists {
		files, err := parseFiles(filesVal)
		if err != nil {
			return Info{}, nil, err
		}
		info.Files = files
	}

	infoBytes, err := extractInfoBytes(originalData)
	if err != nil {
		return Info{}, nil, newValidationError(ErrInvalidInfoDict, "info_bytes",
			fmt.Sprintf("failed to extract info bytes: %v", err))
	}

	return info, infoBytes, nil
}

// parseFiles parses the files array for multi-file torrents.
func parseFiles(value any) ([]File, error) {
	filesList, ok := value.([]any)
	if !ok {
		return nil, newValidationError(ErrInvalidMultiFile, "files",
			fmt.Sprintf("files must be a list, got %T", value))
	}

	files := make([]File, 0, len(filesList))

	for i, fileVal := range filesList {
		fileDict, ok := fileVal.(map[string]any)
		if !ok {
			return nil, newValidationError(ErrInvalidMultiFile, "files",
				fmt.Sprintf("files[%d] must be a dictionary, got %T", i, fileVal))
		}

		var file File

		lengthVal, exists := fileDict["length"]
		if !exists {
			return nil, newValidationError(ErrInvalidMultiFile, "files",
				fmt.Sprintf("files[%d] missing required length field", i))
		}
		lengthInt, ok := lengthVal.(int64)
		if !ok {
			return nil, newValidationError(ErrInvalidMultiFile, "files",
				fmt.Sprintf("files[%d] length must be an integer, got %T", i, lengthVal))
		}
		file.Length = lengthInt

		pathVal, exists := fileDict["path"]
		if !exists {
			return nil, newValidationError(ErrInvalidFilePath, "files",
				fmt.Sprintf("files[%d] missing required path field", i))
		}
		pathList, ok := pathVal.([]any)
		if !ok {
			return nil, newValidationError(ErrInvalidFilePath, "files",
				fmt.Sprintf("files[%d] path must be a list, got %T", i, pathVal))
		}

		path := make([]string, 0, len(pathList))
		for j, pathComponentVal := range pathList {
			pathComponentStr, err := bytesToString(pathComponentVal)
			if err != nil {
				return nil, newValidationError(ErrInvalidFilePath, "files",
					fmt.Sprintf("files[%d] path[%d] must be a string, got %T", i, j, pathComponentVal))
			}
			path = append(path, pathComponentStr)
		}
		file.Path = path

		if md5Val, exists := fileDict["md5sum"]; exists {
			if md5Str, err := bytesToString(md5Val); err == nil {
				file.MD5Sum = md5Str
			}
		}

		files = append(files, file)
	}

	return files, nil
}

// extractInfoBytes extracts the raw bencode bytes of the info dictionary.
func extractInfoBytes(data []byte) ([]byte, error) {
	value, _, err := bencode.Decode(data)
	if err != nil {
		return nil, fmt.Errorf("failed to decode data: %w", err)
	}

	dict, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("expected dictionary at root level")
	}

	infoVal, exists := dict["info"]
	if !exists {
		return nil, errors.New("no info dictionary found")
	}

	encodedBytes, err := bencode.Encode(infoVal)
	if err != nil {
		return nil, fmt.Errorf("failed to re-encode info dictionary: %w", err)
	}

	return encodedBytes, nil
}
