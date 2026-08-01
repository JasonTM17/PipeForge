package upload

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

const SniffBytes = 64 * 1024

type Format string

const (
	FormatCSV     Format = "CSV"
	FormatJSONL   Format = "JSONL"
	FormatParquet Format = "PARQUET"
)

var (
	ErrUnsafeFilename    = errors.New("unsafe filename")
	ErrUnsupportedFormat = errors.New("unsupported dataset format")
	ErrInvalidContent    = errors.New("content does not match the declared dataset format")
	ErrInvalidChecksum   = errors.New("checksum must be a SHA-256 hex digest")
)

var checksumPattern = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)

func NormalizeFilename(value string) (string, error) {
	name := strings.TrimSpace(value)
	if name == "" || len(name) > 255 || name == "." || name == ".." || strings.ContainsAny(name, "/\\") {
		return "", ErrUnsafeFilename
	}
	if filepath.Base(name) != name {
		return "", ErrUnsafeFilename
	}
	for _, runeValue := range name {
		if runeValue < 0x20 || runeValue == 0x7f {
			return "", ErrUnsafeFilename
		}
	}
	return name, nil
}

func DetectFormat(filename, contentType string, prefix []byte) (Format, error) {
	format, err := DetectDeclaredFormat(filename, contentType)
	if err != nil {
		return "", err
	}
	if err := validatePrefix(format, prefix); err != nil {
		return "", err
	}
	return format, nil
}

func DetectDeclaredFormat(filename, contentType string) (Format, error) {
	name, err := NormalizeFilename(filename)
	if err != nil {
		return "", err
	}
	byExtension := formatFromExtension(filepath.Ext(name))
	byContentType := formatFromContentType(contentType)
	if byExtension != "" && byContentType != "" && byExtension != byContentType {
		return "", fmt.Errorf("%w: extension and content type disagree", ErrUnsupportedFormat)
	}
	format := byExtension
	if format == "" {
		format = byContentType
	}
	if format == "" {
		return "", ErrUnsupportedFormat
	}
	return format, nil
}

func NormalizeChecksum(value string) (string, error) {
	checksum := strings.ToLower(strings.TrimSpace(value))
	if !checksumPattern.MatchString(checksum) {
		return "", ErrInvalidChecksum
	}
	return checksum, nil
}

func ChecksumHex(sum [sha256.Size]byte) string {
	return hex.EncodeToString(sum[:])
}

func ChecksumBytes(sum []byte) string {
	return hex.EncodeToString(sum)
}

func formatFromExtension(extension string) Format {
	switch strings.ToLower(extension) {
	case ".csv":
		return FormatCSV
	case ".jsonl", ".ndjson":
		return FormatJSONL
	case ".parquet":
		return FormatParquet
	default:
		return ""
	}
}

func formatFromContentType(contentType string) Format {
	base := strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	switch base {
	case "text/csv", "application/csv":
		return FormatCSV
	case "application/json", "application/jsonl", "application/x-ndjson", "text/json":
		return FormatJSONL
	case "application/parquet", "application/x-parquet", "application/vnd.apache.parquet":
		return FormatParquet
	default:
		return ""
	}
}

func validatePrefix(format Format, prefix []byte) error {
	if len(prefix) == 0 {
		return ErrInvalidContent
	}
	switch format {
	case FormatParquet:
		if len(prefix) < 4 || !bytes.Equal(prefix[:4], []byte("PAR1")) {
			return ErrInvalidContent
		}
		return nil
	case FormatCSV:
		if !utf8.Valid(prefix) || bytes.IndexByte(prefix, 0) >= 0 {
			return ErrInvalidContent
		}
		if bytes.IndexByte(prefix, ',') < 0 && bytes.IndexByte(prefix, ';') < 0 && bytes.IndexByte(prefix, '\t') < 0 && bytes.IndexByte(prefix, '\n') < 0 && bytes.IndexByte(prefix, '\r') < 0 {
			return ErrInvalidContent
		}
		return nil
	case FormatJSONL:
		if !utf8.Valid(prefix) || bytes.IndexByte(prefix, 0) >= 0 {
			return ErrInvalidContent
		}
		line := prefix
		if index := bytes.IndexByte(prefix, '\n'); index >= 0 {
			line = prefix[:index]
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 || (len(prefix) < SniffBytes && !json.Valid(line)) {
			return ErrInvalidContent
		}
		return nil
	default:
		return ErrUnsupportedFormat
	}
}
