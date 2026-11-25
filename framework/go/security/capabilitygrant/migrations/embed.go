package migrations

import "embed"

// Files 是 Framework capability grant 技术表迁移集合。
//
//go:embed *.sql
var Files embed.FS
