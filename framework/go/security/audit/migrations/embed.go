package migrations

import "embed"

// Files 是 Framework audit 技术表迁移集合。
//
//go:embed *.sql
var Files embed.FS
