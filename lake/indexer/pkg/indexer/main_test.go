package indexer

import (
	"context"
	"os"
	"testing"

	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
	clickhousetesting "github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse/testing"
	laketesting "github.com/malbeclabs/doublezero/lake/utils/pkg/testing"
)

var (
	sharedDB *clickhousetesting.DB
)

func TestMain(m *testing.M) {
	log := laketesting.NewLogger()
	var err error
	sharedDB, err = clickhousetesting.NewDB(context.Background(), log, nil)
	if err != nil {
		log.Error("failed to create shared DB", "error", err)
		os.Exit(1)
	}
	code := m.Run()
	sharedDB.Close()
	os.Exit(code)
}

func testClient(t *testing.T) clickhouse.Client {
	client := laketesting.NewClient(t, sharedDB)
	return client
}
