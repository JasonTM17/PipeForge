package storage

import (
	"context"
	"errors"
	"io"
	"time"
)

var ErrObjectNotFound = errors.New("object not found")
var ErrMultipartPartNotFound = errors.New("multipart part not found")

type ObjectInfo struct {
	Key         string
	Size        int64
	ETag        string
	ContentType string
}

type ObjectStore interface {
	Put(context.Context, string, io.Reader, int64, string) (ObjectInfo, error)
	Head(context.Context, string) (ObjectInfo, error)
	Get(context.Context, string) (io.ReadCloser, error)
	Delete(context.Context, string) error
}

type MultipartPart struct {
	PartNumber int
	ETag       string
	Size       int64
}

type MultipartStore interface {
	InitiateMultipart(context.Context, string, string) (string, error)
	PresignPart(context.Context, string, string, int, time.Duration) (string, error)
	ListMultipartParts(context.Context, string, string) ([]MultipartPart, error)
	GetMultipartPart(context.Context, string, string, int) (MultipartPart, error)
	CompleteMultipart(context.Context, string, string, []MultipartPart, string) (ObjectInfo, error)
	AbortMultipart(context.Context, string, string) error
}
