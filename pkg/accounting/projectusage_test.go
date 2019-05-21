// Copyright (C) 2019 Storj Labs, Inc.
// See LICENSE for copying information.

package accounting_test

import (
	"crypto/rand"
	"fmt"
	"testing"
	"time"

	"github.com/skyrings/skyring-common/tools/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"storj.io/storj/internal/memory"
	"storj.io/storj/internal/testcontext"
	"storj.io/storj/internal/testplanet"
	"storj.io/storj/pkg/accounting"
	"storj.io/storj/pkg/pb"
	"storj.io/storj/pkg/storj"
	"storj.io/storj/satellite"
	"storj.io/storj/satellite/orders"
	"storj.io/storj/satellite/satellitedb/satellitedbtest"
)

func TestProjectUsageStorage(t *testing.T) {
	cases := []struct {
		name             string
		expectedExceeded bool
		expectedResource string
		expectedErrMsg   string
	}{
		{name: "doesn't exceed storage or bandwidth project limit", expectedExceeded: false, expectedErrMsg: ""},
		{name: "exceeds storage project limit", expectedExceeded: true, expectedResource: "storage", expectedErrMsg: "segment error: metainfo error: rpc error: code = ResourceExhausted desc = Exceeded Alpha Usage Limit; segment error: metainfo error: rpc error: code = ResourceExhausted desc = Exceeded Alpha Usage Limit"},
	}

	testplanet.Run(t, testplanet.Config{
		SatelliteCount: 1, StorageNodeCount: 6, UplinkCount: 1,
	}, func(t *testing.T, ctx *testcontext.Context, planet *testplanet.Planet) {
		saDB := planet.Satellites[0].DB
		acctDB := saDB.ProjectAccounting()

		// Setup: create a new project to use the projectID
		projects, err := planet.Satellites[0].DB.Console().Projects().GetAll(ctx)
		projectID := projects[0].ID
		require.NoError(t, err)

		for _, tt := range cases {
			t.Run(tt.name, func(t *testing.T) {

				// Setup: create BucketStorageTally records to test exceeding storage project limit
				if tt.expectedResource == "storage" {
					now := time.Now()
					err := setUpStorageTallies(ctx, projectID, acctDB, now)
					require.NoError(t, err)
				}

				// Execute test: get storage totals for a project, then check if that exceeds the max usage limit
				inlineTotal, remoteTotal, err := acctDB.GetStorageTotals(ctx, projectID)
				require.NoError(t, err)
				maxAlphaUsage := 25 * memory.GB
				actualExceeded, actualResource := accounting.ExceedsAlphaUsage(0, inlineTotal, remoteTotal, maxAlphaUsage)
				require.Equal(t, tt.expectedExceeded, actualExceeded)
				require.Equal(t, tt.expectedResource, actualResource)

				// Setup: create some bytes for the uplink to upload
				expectedData := make([]byte, 50*memory.KiB)
				_, err = rand.Read(expectedData)
				require.NoError(t, err)

				// Execute test: check that the uplink gets an error when they have exceeded storage limits and try to upload a file
				actualErr := planet.Uplinks[0].Upload(ctx, planet.Satellites[0], "testbucket", "test/path", expectedData)
				if tt.expectedResource == "storage" {
					assert.EqualError(t, actualErr, tt.expectedErrMsg)
				} else {
					require.NoError(t, actualErr)
				}
			})
		}
	})
}

func TestProjectUsageBandwidth(t *testing.T) {
	cases := []struct {
		name             string
		expectedExceeded bool
		expectedResource string
		expectedErrMsg   string
	}{
		{name: "doesn't exceed storage or bandwidth project limit", expectedExceeded: false, expectedErrMsg: ""},
		{name: "exceeds bandwidth project limit", expectedExceeded: true, expectedResource: "bandwidth", expectedErrMsg: "segment error: metainfo error: rpc error: code = ResourceExhausted desc = Exceeded Alpha Usage Limit"},
	}

	testplanet.Run(t, testplanet.Config{
		SatelliteCount: 1, StorageNodeCount: 6, UplinkCount: 1,
	}, func(t *testing.T, ctx *testcontext.Context, planet *testplanet.Planet) {
		saDB := planet.Satellites[0].DB
		orderDB := saDB.Orders()
		acctDB := saDB.ProjectAccounting()

		// Setup: get projectID and create bucketID
		projects, err := planet.Satellites[0].DB.Console().Projects().GetAll(ctx)
		projectID := projects[0].ID
		require.NoError(t, err)
		bucketName := "testbucket"
		bucketID := createBucketID(projectID, []byte(bucketName))

		for _, tt := range cases {
			t.Run(tt.name, func(t *testing.T) {

				// Setup: create a BucketBandwidthRollup record to test exceeding bandwidth project limit
				if tt.expectedResource == "bandwidth" {
					now := time.Now().UTC()
					err := setUpBucketBandwidthAllocations(ctx, projectID, orderDB, now)
					require.NoError(t, err)
				}

				// Setup: create some bytes for the uplink to upload to test the download later
				expectedData := make([]byte, 50*memory.KiB)
				_, err = rand.Read(expectedData)
				require.NoError(t, err)
				err := planet.Uplinks[0].Upload(ctx, planet.Satellites[0], bucketName, "test/path", expectedData)
				require.NoError(t, err)

				// Setup: This date represents the past 30 days so that we can check
				// if the alpha max usage has been exceeded in the past month
				from := time.Now().AddDate(0, 0, -accounting.AverageDaysInMonth)

				// Execute test: get bandwidth totals for a project, then check if that exceeds the max usage limit
				bandwidthTotal, err := acctDB.GetAllocatedBandwidthTotal(ctx, bucketID, from)
				require.NoError(t, err)
				maxAlphaUsage := 25 * memory.GB
				actualExceeded, actualResource := accounting.ExceedsAlphaUsage(bandwidthTotal, 0, 0, maxAlphaUsage)
				require.Equal(t, tt.expectedExceeded, actualExceeded)
				require.Equal(t, tt.expectedResource, actualResource)

				// Execute test: check that the uplink gets an error when they have exceeded bandwidth limits and try to download a file
				_, actualErr := planet.Uplinks[0].Download(ctx, planet.Satellites[0], bucketName, "test/path")
				if tt.expectedResource == "bandwidth" {
					assert.EqualError(t, actualErr, tt.expectedErrMsg)
				} else {
					require.NoError(t, actualErr)
				}

			})
		}
	})
}

func createBucketID(projectID uuid.UUID, bucket []byte) []byte {
	entries := make([]string, 0)
	entries = append(entries, projectID.String())
	entries = append(entries, string(bucket))
	return []byte(storj.JoinPaths(entries...))
}

func setUpStorageTallies(ctx *testcontext.Context, projectID uuid.UUID, acctDB accounting.ProjectAccounting, time time.Time) error {

	// Create many records that sum greater than project usage limit of 25GB
	for i := 0; i < 4; i++ {
		bucketName := fmt.Sprintf("%s%d", "testbucket", i)
		tally := accounting.BucketStorageTally{
			BucketName:    bucketName,
			ProjectID:     projectID,
			IntervalStart: time,

			// In order to exceed the project limits, create storage tally records
			// that sum greater than the maxAlphaUsage * expansionFactor
			RemoteBytes: 10 * memory.GB.Int64() * accounting.ExpansionFactor,
		}
		err := acctDB.CreateStorageTally(ctx, tally)
		if err != nil {
			return err
		}
	}
	return nil
}

func createBucketBandwidthRollups(ctx *testcontext.Context, satelliteDB satellite.DB, projectID uuid.UUID) (int64, error) {
	var expectedSum int64
	ordersDB := satelliteDB.Orders()
	amount := int64(1000)
	now := time.Now()

	for i := 0; i < 4; i++ {
		var bucketName string
		var intervalStart time.Time
		if i%2 == 0 {
			// When the bucket name and intervalStart is different, a new record is created
			bucketName = fmt.Sprintf("%s%d", "testbucket", i)
			// Use a intervalStart time in the past to test we get all records in past 30 days
			intervalStart = now.AddDate(0, 0, -i)
		} else {
			// When the bucket name and intervalStart is the same, we update the existing record
			bucketName = "testbucket"
			intervalStart = now
		}

		bucketID := createBucketID(projectID, []byte(bucketName))
		err := ordersDB.UpdateBucketBandwidthAllocation(ctx,
			bucketID, pb.PieceAction_GET, amount, intervalStart,
		)
		if err != nil {
			return expectedSum, err
		}
		err = ordersDB.UpdateBucketBandwidthSettle(ctx,
			bucketID, pb.PieceAction_GET, amount, intervalStart,
		)
		if err != nil {
			return expectedSum, err
		}
		err = ordersDB.UpdateBucketBandwidthInline(ctx,
			bucketID, pb.PieceAction_GET, amount, intervalStart,
		)
		if err != nil {
			return expectedSum, err
		}
		expectedSum += amount
	}
	return expectedSum, nil
}

func TestProjectBandwidthTotal(t *testing.T) {
	satellitedbtest.Run(t, func(t *testing.T, db satellite.DB) {
		ctx := testcontext.New(t)
		defer ctx.Cleanup()

		pdb := db.ProjectAccounting()
		projectID, err := uuid.New()
		require.NoError(t, err)

		// Setup: create bucket bandwidth rollup records
		expectedTotal, err := createBucketBandwidthRollups(ctx, db, *projectID)
		require.NoError(t, err)

		// Execute test: get project bandwidth total
		bucketID := createBucketID(*projectID, []byte("testbucket"))
		from := time.Now().AddDate(0, 0, -accounting.AverageDaysInMonth) // past 30 days
		actualBandwidthTotal, err := pdb.GetAllocatedBandwidthTotal(ctx, bucketID, from)
		require.NoError(t, err)
		require.Equal(t, actualBandwidthTotal, expectedTotal)
	})
}

func setUpBucketBandwidthAllocations(ctx *testcontext.Context, projectID uuid.UUID, orderDB orders.DB, now time.Time) error {

	// Create many records that sum greater than project usage limit of 25GB
	for i := 0; i < 4; i++ {
		bucketName := fmt.Sprintf("%s%d", "testbucket", i)
		bucketID := createBucketID(projectID, []byte(bucketName))

		// In order to exceed the project limits, create bandwidth allocation records
		// that sum greater than the maxAlphaUsage * expansionFactor
		amount := 10 * memory.GB.Int64() * accounting.ExpansionFactor
		action := pb.PieceAction_GET
		intervalStart := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, now.Location())
		err := orderDB.UpdateBucketBandwidthAllocation(ctx, bucketID, action, amount, intervalStart)
		if err != nil {
			return err
		}
	}
	return nil
}
