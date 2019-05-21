// Copyright (C) 2019 Storj Labs, Inc.
// See LICENSE for copying information.

package collector_test

import (
	"crypto/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"storj.io/storj/internal/memory"
	"storj.io/storj/internal/testcontext"
	"storj.io/storj/internal/testplanet"
)

func TestCollector(t *testing.T) {
	testplanet.Run(t, testplanet.Config{
		SatelliteCount: 1, StorageNodeCount: 3, UplinkCount: 1,
	}, func(t *testing.T, ctx *testcontext.Context, planet *testplanet.Planet) {
		for _, storageNode := range planet.StorageNodes {
			// stop collector, so we can run it manually
			storageNode.Collector.Loop.Pause()
			// stop order sender because we will stop satellite later
			storageNode.Storage2.Sender.Loop.Pause()
		}

		expectedData := make([]byte, 100*memory.KiB)
		_, err := rand.Read(expectedData)
		require.NoError(t, err)

		// upload some data that expires in 8 days
		err = planet.Uplinks[0].UploadWithExpiration(ctx,
			planet.Satellites[0], "testbucket", "test/path",
			expectedData, time.Now().Add(8*24*time.Hour))
		require.NoError(t, err)

		// stop planet to prevent audits
		require.NoError(t, planet.StopPeer(planet.Satellites[0]))

		collections := 0

		// imagine we are 16 days in the future
		for _, storageNode := range planet.StorageNodes {
			pieceinfos := storageNode.DB.PieceInfo()

			// verify that we actually have some data on storage nodes
			used, err := pieceinfos.SpaceUsed(ctx)
			require.NoError(t, err)
			if used == 0 {
				// this storage node didn't get picked for storing data
				continue
			}

			// collect all the data
			err = storageNode.Collector.Collect(ctx, time.Now().Add(16*24*time.Hour))
			require.NoError(t, err)

			// verify that we deleted everything
			used, err = pieceinfos.SpaceUsed(ctx)
			require.NoError(t, err)
			require.Equal(t, int64(0), used)

			collections++
		}

		require.NotZero(t, collections)
	})
}
