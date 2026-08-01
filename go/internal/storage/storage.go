package storage

import (
	"context"
	"io"
)

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
