package migrations

import "embed"

// Files 是 Task Engine 自有的只向前技术表迁移集合。
//
//go:embed *.sql
var Files embed.FS
