package indexer

import (
	"context"
	"fmt"
	"time"

	"github.com/malbeclabs/doublezero/lake/pkg/duck"
	dzsvc "github.com/malbeclabs/doublezero/lake/pkg/indexer/dz/serviceability"
	mcpgeoip "github.com/malbeclabs/doublezero/lake/pkg/indexer/geoip"
	"github.com/malbeclabs/doublezero/lake/pkg/indexer/sol"
)

// createAllSCD2Tables creates all SCD2 tables before validation
// This acts as a schema migration to ensure tables exist with the correct structure
func (i *Indexer) createAllSCD2Tables(ctx context.Context) error {
	conn, err := i.cfg.DB.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()

	// Get base configs from each store package
	configs := []duck.SCDTableConfig{
		// GeoIP
		mcpgeoip.SCD2ConfigGeoIPRecords(),
		// Solana
		sol.SCD2ConfigLeaderSchedule(),
		sol.SCD2ConfigVoteAccounts(),
		sol.SCD2ConfigGossipNodes(),
		// Serviceability
		dzsvc.SCD2ConfigContributors(),
		dzsvc.SCD2ConfigDevices(),
		dzsvc.SCD2ConfigUsers(),
		dzsvc.SCD2ConfigMetros(),
		dzsvc.SCD2ConfigLinks(),
	}

	for _, cfg := range configs {
		// Set dummy values for fields not needed for table creation
		cfg.SnapshotTS = time.Now().UTC()
		cfg.RunID = ""

		if err := duck.CreateSCDTables(ctx, i.log, conn, cfg); err != nil {
			return fmt.Errorf("failed to create SCD2 tables for %s: %w", cfg.TableBaseName, err)
		}
	}

	return nil
}
