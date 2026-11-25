package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestDefaultPoolRetainsConnectionsAfterConcurrentQueries(t *testing.T) {
	for _, test := range []struct {
		name                   string
		maxOpen, maxIdle, held int
		wantIdle, wantClosed   int
	}{
		{name: "默认连接复用", held: 4, wantIdle: 4},
		{name: "单连接上限", maxOpen: 1, held: 1, wantIdle: 1},
		{name: "显式空闲上限", maxOpen: 4, maxIdle: 1, held: 4, wantIdle: 1, wantClosed: 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, err := Open(context.Background(), Options{
				Path: filepath.Join(t.TempDir(), "pool.db"), MaxOpenConns: test.maxOpen, MaxIdleConns: test.maxIdle,
				Migrations: fstest.MapFS{"0001_values.sql": &fstest.MapFile{Data: []byte("CREATE TABLE pool_values(id TEXT);")}},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer Close(db)
			before := db.Stats()
			connections := make([]*sql.Conn, 0, test.held)
			for i := 0; i < test.held; i++ {
				conn, err := db.Conn(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				connections = append(connections, conn)
				defer conn.Close()
				if _, err := conn.ExecContext(context.Background(), "SELECT count(*) FROM pool_values"); err != nil {
					t.Fatal(err)
				}
			}
			for _, conn := range connections {
				if err := conn.Close(); err != nil {
					t.Fatal(err)
				}
			}
			after := db.Stats()
			if after.Idle != test.wantIdle || after.MaxIdleClosed-before.MaxIdleClosed != int64(test.wantClosed) {
				t.Fatalf("空闲连接=%d，销毁=%d；期望 %d / %d", after.Idle, after.MaxIdleClosed-before.MaxIdleClosed, test.wantIdle, test.wantClosed)
			}
		})
	}
}
