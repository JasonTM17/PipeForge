package upload

import (
	"crypto/sha256"
	"errors"
	"testing"
)

func TestNormalizeFilenameRejectsTraversalAndControlCharacters(t *testing.T) {
	for _, value := range []string{"../data.csv", `folder\data.csv`, "..", "bad\nname.csv"} {
		if _, err := NormalizeFilename(value); !errors.Is(err, ErrUnsafeFilename) {
			t.Fatalf("expected unsafe filename rejection for %q, got %v", value, err)
		}
	}
}

func TestDetectFormatUsesExtensionContentTypeAndMagic(t *testing.T) {
	format, err := DetectFormat("records.csv", "text/csv", []byte("id,name\n1,Ada\n"))
	if err != nil || format != FormatCSV {
		t.Fatalf("unexpected CSV detection: %s %v", format, err)
	}
	format, err = DetectFormat("events.jsonl", "application/x-ndjson", []byte(`{"id":1}`))
	if err != nil || format != FormatJSONL {
		t.Fatalf("unexpected JSONL detection: %s %v", format, err)
	}
	format, err = DetectFormat("data.parquet", "application/octet-stream", []byte("PAR1binary"))
	if err != nil || format != FormatParquet {
		t.Fatalf("unexpected Parquet detection: %s %v", format, err)
	}
	if _, err := DetectFormat("records.csv", "application/json", []byte(`{"id":1}`)); !errors.Is(err, ErrUnsupportedFormat) {
		t.Fatalf("expected extension/content-type mismatch, got %v", err)
	}
}

func TestDetectDeclaredFormatValidatesMultipartMetadata(t *testing.T) {
	format, err := DetectDeclaredFormat("events.csv", "text/csv")
	if err != nil || format != FormatCSV {
		t.Fatalf("unexpected declared format=%q err=%v", format, err)
	}
	if _, err := DetectDeclaredFormat("events.csv", "application/json"); !errors.Is(err, ErrUnsupportedFormat) {
		t.Fatalf("expected extension/content-type mismatch, got %v", err)
	}
}

func TestChecksumNormalization(t *testing.T) {
	sum := sha256.Sum256([]byte("pipeforge"))
	value, err := NormalizeChecksum(ChecksumHex(sum))
	if err != nil || value != ChecksumHex(sum) {
		t.Fatalf("unexpected checksum normalization: %s %v", value, err)
	}
	if _, err := NormalizeChecksum("not-a-checksum"); !errors.Is(err, ErrInvalidChecksum) {
		t.Fatalf("expected invalid checksum rejection, got %v", err)
	}
}
