package migrations

import "embed"

// Files contains versioned SQL migrations shipped with the control plane.
// The migration runner applies them in filename order.
//
//go:embed *.sql
var Files embed.FS
