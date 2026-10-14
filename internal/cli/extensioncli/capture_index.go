package extensioncli

import (
	"database/sql"

	capturecli "github.com/monoes/mono-agent/internal/cli/capture"
	"github.com/monoes/mono-agent/internal/cli/capturehooks"
)

// Capture indexing in the bridge: see internal/cli/capturehooks.

// captureIndexStartupDelay is how long a bridge waits after starting
// before it retries every profile's unindexed captures: long enough that
// indexing never competes with the bridge and the daemon coming up.
const captureIndexStartupDelay = capturehooks.IndexStartupDelay

// openCaptureIndexDB opens the database for one indexing pass.
func openCaptureIndexDB() (*sql.DB, func(), error) {
	db, err := capturecli.OpenProfileDB(capturecli.DefaultDBPath)
	if err != nil {
		return nil, nil, err
	}
	return db.DB, func() { db.Close() }, nil
}
