// Copyright (C) 2019 Storj Labs, Inc.
// See LICENSE for copying information.

package metainfo

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/skyrings/skyring-common/tools/uuid"
	"github.com/zeebo/errs"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	monkit "gopkg.in/spacemonkeygo/monkit.v2"

	"storj.io/storj/internal/memory"
	"storj.io/storj/pkg/accounting"
	"storj.io/storj/pkg/accounting/live"
	"storj.io/storj/pkg/auth"
	"storj.io/storj/pkg/eestream"
	"storj.io/storj/pkg/identity"
	"storj.io/storj/pkg/overlay"
	"storj.io/storj/pkg/pb"
	"storj.io/storj/pkg/storj"
	"storj.io/storj/satellite/console"
	"storj.io/storj/satellite/orders"
	"storj.io/storj/storage"
)

var (
	mon = monkit.Package()
	// Error general metainfo error
	Error = errs.Class("metainfo error")
)

// APIKeys is api keys store methods used by endpoint
type APIKeys interface {
	GetByKey(ctx context.Context, key console.APIKey) (*console.APIKeyInfo, error)
}

// Endpoint metainfo endpoint
type Endpoint struct {
	log                     *zap.Logger
	metainfo                *Service
	orders                  *orders.Service
	cache                   *overlay.Cache
	apiKeys                 APIKeys
	storagenodeAccountingDB accounting.StoragenodeAccounting
	projectAccountingDB     accounting.ProjectAccounting
	liveAccounting          live.Service
	maxAlphaUsage           memory.Size
}

// NewEndpoint creates new metainfo endpoint instance
func NewEndpoint(log *zap.Logger, metainfo *Service, orders *orders.Service, cache *overlay.Cache, apiKeys APIKeys, sdb accounting.StoragenodeAccounting, pdb accounting.ProjectAccounting, liveAccounting live.Service, maxAlphaUsage memory.Size) *Endpoint {
	// TODO do something with too many params
	return &Endpoint{
		log:                     log,
		metainfo:                metainfo,
		orders:                  orders,
		cache:                   cache,
		apiKeys:                 apiKeys,
		storagenodeAccountingDB: sdb,
		projectAccountingDB:     pdb,
		liveAccounting:          liveAccounting,
		maxAlphaUsage:           maxAlphaUsage,
	}
}

// Close closes resources
func (endpoint *Endpoint) Close() error { return nil }

func (endpoint *Endpoint) validateAuth(ctx context.Context) (*console.APIKeyInfo, error) {
	APIKey, ok := auth.GetAPIKey(ctx)
	if !ok {
		endpoint.log.Error("unauthorized request: ", zap.Error(status.Errorf(codes.Unauthenticated, "Invalid API credential")))
		return nil, status.Errorf(codes.Unauthenticated, "Invalid API credential")
	}

	key, err := console.APIKeyFromBase32(string(APIKey))
	if err != nil {
		endpoint.log.Error("unauthorized request: ", zap.Error(status.Errorf(codes.Unauthenticated, "Invalid API credential")))
		return nil, status.Errorf(codes.Unauthenticated, "Invalid API credential")
	}

	keyInfo, err := endpoint.apiKeys.GetByKey(ctx, *key)
	if err != nil {
		endpoint.log.Error("unauthorized request: ", zap.Error(status.Errorf(codes.Unauthenticated, err.Error())))
		return nil, status.Errorf(codes.Unauthenticated, "Invalid API credential")
	}

	return keyInfo, nil
}

// SegmentInfo returns segment metadata info
func (endpoint *Endpoint) SegmentInfo(ctx context.Context, req *pb.SegmentInfoRequest) (resp *pb.SegmentInfoResponse, err error) {
	defer mon.Task()(&ctx)(&err)

	keyInfo, err := endpoint.validateAuth(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, err.Error())
	}

	err = endpoint.validateBucket(req.Bucket)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, err.Error())
	}

	path, err := CreatePath(keyInfo.ProjectID, req.Segment, req.Bucket, req.Path)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, err.Error())
	}

	// TODO refactor to use []byte directly
	pointer, err := endpoint.metainfo.Get(path)
	if err != nil {
		if storage.ErrKeyNotFound.Has(err) {
			return nil, status.Errorf(codes.NotFound, err.Error())
		}
		return nil, status.Errorf(codes.Internal, err.Error())
	}

	return &pb.SegmentInfoResponse{Pointer: pointer}, nil
}

// CreateSegment will generate requested number of OrderLimit with coresponding node addresses for them
func (endpoint *Endpoint) CreateSegment(ctx context.Context, req *pb.SegmentWriteRequest) (resp *pb.SegmentWriteResponse, err error) {
	defer mon.Task()(&ctx)(&err)

	keyInfo, err := endpoint.validateAuth(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, err.Error())
	}

	err = endpoint.validateBucket(req.Bucket)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, err.Error())
	}

	err = endpoint.validateRedundancy(req.Redundancy)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, err.Error())
	}

	// Check if this projectID has exceeded alpha usage limits, i.e. 25GB of bandwidth or storage used in the past month
	// TODO: remove this code once we no longer need usage limiting for alpha release
	// Ref: https://storjlabs.atlassian.net/browse/V3-1274
	inlineTotal, remoteTotal, err := endpoint.getProjectStorageTotals(ctx, keyInfo.ProjectID)
	if err != nil {
		endpoint.log.Error("retrieving project storage totals", zap.Error(err))

	}
	exceeded, resource := accounting.ExceedsAlphaUsage(0, inlineTotal, remoteTotal, endpoint.maxAlphaUsage)
	if exceeded {
		endpoint.log.Sugar().Errorf("monthly project limits are %s of storage and bandwidth usage. This limit has been exceeded for %s for projectID %s.",
			endpoint.maxAlphaUsage.String(),
			resource, keyInfo.ProjectID,
		)
		return nil, status.Errorf(codes.ResourceExhausted, "Exceeded Alpha Usage Limit")
	}

	redundancy, err := eestream.NewRedundancyStrategyFromProto(req.GetRedundancy())
	if err != nil {
		return nil, err
	}

	maxPieceSize := eestream.CalcPieceSize(req.GetMaxEncryptedSegmentSize(), redundancy)

	request := overlay.FindStorageNodesRequest{
		RequestedCount: int(req.Redundancy.Total),
		FreeBandwidth:  maxPieceSize,
		FreeDisk:       maxPieceSize,
	}
	nodes, err := endpoint.cache.FindStorageNodes(ctx, request)
	if err != nil {
		return nil, status.Errorf(codes.Internal, err.Error())
	}

	uplinkIdentity, err := identity.PeerIdentityFromContext(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, err.Error())
	}

	bucketID := createBucketID(keyInfo.ProjectID, req.Bucket)
	rootPieceID, addressedLimits, err := endpoint.orders.CreatePutOrderLimits(ctx, uplinkIdentity, bucketID, nodes, req.Expiration, maxPieceSize)
	if err != nil {
		return nil, Error.Wrap(err)
	}

	return &pb.SegmentWriteResponse{AddressedLimits: addressedLimits, RootPieceId: rootPieceID}, nil
}

func (endpoint *Endpoint) getProjectStorageTotals(ctx context.Context, projectID uuid.UUID) (int64, int64, error) {
	lastCountInline, lastCountRemote, err := endpoint.projectAccountingDB.GetStorageTotals(ctx, projectID)
	if err != nil {
		return 0, 0, err
	}
	rtInline, rtRemote, err := endpoint.liveAccounting.GetProjectStorageUsage(ctx, projectID)
	if err != nil {
		return 0, 0, err
	}
	return lastCountInline + rtInline, lastCountRemote + rtRemote, nil
}

func calculateSpaceUsed(ptr *pb.Pointer) (inlineSpace, remoteSpace int64) {
	inline := ptr.GetInlineSegment()
	if inline != nil {
		return int64(len(inline)), 0
	}
	segmentSize := ptr.GetSegmentSize()
	remote := ptr.GetRemote()
	if remote == nil {
		return 0, 0
	}
	minReq := remote.GetRedundancy().GetMinReq()
	pieceSize := segmentSize / int64(minReq)
	pieces := remote.GetRemotePieces()
	return 0, pieceSize * int64(len(pieces))
}

// CommitSegment commits segment metadata
func (endpoint *Endpoint) CommitSegment(ctx context.Context, req *pb.SegmentCommitRequest) (resp *pb.SegmentCommitResponse, err error) {
	defer mon.Task()(&ctx)(&err)

	keyInfo, err := endpoint.validateAuth(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, err.Error())
	}

	err = endpoint.validateBucket(req.Bucket)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, err.Error())
	}

	err = endpoint.validateCommit(req)
	if err != nil {
		return nil, status.Errorf(codes.Internal, err.Error())
	}

	err = endpoint.filterValidPieces(req.Pointer)
	if err != nil {
		return nil, status.Errorf(codes.Internal, err.Error())
	}

	path, err := CreatePath(keyInfo.ProjectID, req.Segment, req.Bucket, req.Path)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, err.Error())
	}

	inlineUsed, remoteUsed := calculateSpaceUsed(req.Pointer)
	if err := endpoint.liveAccounting.AddProjectStorageUsage(ctx, keyInfo.ProjectID, inlineUsed, remoteUsed); err != nil {
		endpoint.log.Sugar().Errorf("Could not track new storage usage by project %v: %v", keyInfo.ProjectID, err)
		// but continue. it's most likely our own fault that we couldn't track it, and the only thing
		// that will be affected is our per-project bandwidth and storage limits.
	}

	err = endpoint.metainfo.Put(path, req.Pointer)
	if err != nil {
		return nil, status.Errorf(codes.Internal, err.Error())
	}

	if req.Pointer.Type == pb.Pointer_INLINE {
		bucketID := createBucketID(keyInfo.ProjectID, req.Bucket)
		// TODO or maybe use pointer.SegmentSize ??
		err = endpoint.orders.UpdatePutInlineOrder(ctx, bucketID, int64(len(req.Pointer.InlineSegment)))
		if err != nil {
			return nil, status.Errorf(codes.Internal, err.Error())
		}
	}

	pointer, err := endpoint.metainfo.Get(path)
	if err != nil {
		return nil, status.Errorf(codes.Internal, err.Error())
	}

	return &pb.SegmentCommitResponse{Pointer: pointer}, nil
}

// DownloadSegment gets Pointer incase of INLINE data or list of OrderLimit necessary to download remote data
func (endpoint *Endpoint) DownloadSegment(ctx context.Context, req *pb.SegmentDownloadRequest) (resp *pb.SegmentDownloadResponse, err error) {
	defer mon.Task()(&ctx)(&err)

	keyInfo, err := endpoint.validateAuth(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, err.Error())
	}

	err = endpoint.validateBucket(req.Bucket)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, err.Error())
	}

	// Check if this projectID has exceeded alpha usage limits for bandwidth or storage used in the past month
	// TODO: remove this code once we no longer need usage limiting for alpha release
	// Ref: https://storjlabs.atlassian.net/browse/V3-1274
	bucketID := createBucketID(keyInfo.ProjectID, req.Bucket)
	from := time.Now().AddDate(0, 0, -accounting.AverageDaysInMonth) // past 30 days
	bandwidthTotal, err := endpoint.projectAccountingDB.GetAllocatedBandwidthTotal(ctx, bucketID, from)
	if err != nil {
		endpoint.log.Error("retrieving ProjectBandwidthTotal", zap.Error(err))
	}
	exceeded, resource := accounting.ExceedsAlphaUsage(bandwidthTotal, 0, 0, endpoint.maxAlphaUsage)
	if exceeded {
		endpoint.log.Sugar().Errorf("monthly project usage limit has been exceeded for resource: %s, for project: %d. Contact customer support to increase the limit.",
			resource, keyInfo.ProjectID,
		)
		return nil, status.Errorf(codes.ResourceExhausted, "Exceeded Alpha Usage Limit")
	}

	path, err := CreatePath(keyInfo.ProjectID, req.Segment, req.Bucket, req.Path)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, err.Error())
	}

	// TODO refactor to use []byte directly
	pointer, err := endpoint.metainfo.Get(path)
	if err != nil {
		if storage.ErrKeyNotFound.Has(err) {
			return nil, status.Errorf(codes.NotFound, err.Error())
		}
		return nil, status.Errorf(codes.Internal, err.Error())
	}

	if pointer.Type == pb.Pointer_INLINE {
		// TODO or maybe use pointer.SegmentSize ??
		err := endpoint.orders.UpdateGetInlineOrder(ctx, bucketID, int64(len(pointer.InlineSegment)))
		if err != nil {
			return nil, status.Errorf(codes.Internal, err.Error())
		}
		return &pb.SegmentDownloadResponse{Pointer: pointer}, nil
	} else if pointer.Type == pb.Pointer_REMOTE && pointer.Remote != nil {
		uplinkIdentity, err := identity.PeerIdentityFromContext(ctx)
		if err != nil {
			return nil, status.Errorf(codes.Internal, err.Error())
		}
		limits, err := endpoint.orders.CreateGetOrderLimits(ctx, uplinkIdentity, bucketID, pointer)
		if err != nil {
			return nil, status.Errorf(codes.Internal, err.Error())
		}

		return &pb.SegmentDownloadResponse{Pointer: pointer, AddressedLimits: limits}, nil
	}

	return &pb.SegmentDownloadResponse{}, nil
}

// DeleteSegment deletes segment metadata from satellite and returns OrderLimit array to remove them from storage node
func (endpoint *Endpoint) DeleteSegment(ctx context.Context, req *pb.SegmentDeleteRequest) (resp *pb.SegmentDeleteResponse, err error) {
	defer mon.Task()(&ctx)(&err)

	keyInfo, err := endpoint.validateAuth(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, err.Error())
	}

	err = endpoint.validateBucket(req.Bucket)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, err.Error())
	}

	path, err := CreatePath(keyInfo.ProjectID, req.Segment, req.Bucket, req.Path)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, err.Error())
	}

	// TODO refactor to use []byte directly
	pointer, err := endpoint.metainfo.Get(path)
	if err != nil {
		if storage.ErrKeyNotFound.Has(err) {
			return nil, status.Errorf(codes.NotFound, err.Error())
		}
		return nil, status.Errorf(codes.Internal, err.Error())
	}

	err = endpoint.metainfo.Delete(path)
	if err != nil {
		return nil, status.Errorf(codes.Internal, err.Error())
	}

	if pointer.Type == pb.Pointer_REMOTE && pointer.Remote != nil {
		uplinkIdentity, err := identity.PeerIdentityFromContext(ctx)
		if err != nil {
			return nil, status.Errorf(codes.Internal, err.Error())
		}

		bucketID := createBucketID(keyInfo.ProjectID, req.Bucket)
		limits, err := endpoint.orders.CreateDeleteOrderLimits(ctx, uplinkIdentity, bucketID, pointer)
		if err != nil {
			return nil, status.Errorf(codes.Internal, err.Error())
		}

		return &pb.SegmentDeleteResponse{AddressedLimits: limits}, nil
	}

	return &pb.SegmentDeleteResponse{}, nil
}

// ListSegments returns all Path keys in the Pointers bucket
func (endpoint *Endpoint) ListSegments(ctx context.Context, req *pb.ListSegmentsRequest) (resp *pb.ListSegmentsResponse, err error) {
	defer mon.Task()(&ctx)(&err)

	keyInfo, err := endpoint.validateAuth(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, err.Error())
	}

	prefix, err := CreatePath(keyInfo.ProjectID, -1, req.Bucket, req.Prefix)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, err.Error())
	}

	items, more, err := endpoint.metainfo.List(prefix, string(req.StartAfter), string(req.EndBefore), req.Recursive, req.Limit, req.MetaFlags)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "ListV2: %v", err)
	}

	segmentItems := make([]*pb.ListSegmentsResponse_Item, len(items))
	for i, item := range items {
		segmentItems[i] = &pb.ListSegmentsResponse_Item{
			Path:     []byte(item.Path),
			Pointer:  item.Pointer,
			IsPrefix: item.IsPrefix,
		}
	}

	return &pb.ListSegmentsResponse{Items: segmentItems, More: more}, nil
}

func createBucketID(projectID uuid.UUID, bucket []byte) []byte {
	entries := make([]string, 0)
	entries = append(entries, projectID.String())
	entries = append(entries, string(bucket))
	return []byte(storj.JoinPaths(entries...))
}

func (endpoint *Endpoint) filterValidPieces(pointer *pb.Pointer) error {
	if pointer.Type == pb.Pointer_REMOTE {
		var remotePieces []*pb.RemotePiece
		remote := pointer.Remote
		for _, piece := range remote.RemotePieces {
			// TODO enable verification

			// err := auth.VerifyMsg(piece.Hash, piece.NodeId)
			// if err == nil {
			// 	// set to nil after verification to avoid storing in DB
			// 	piece.Hash = nil
			// 	remotePieces = append(remotePieces, piece)
			// } else {
			// 	// TODO satellite should send Delete request for piece that failed
			// 	s.logger.Warn("unable to verify piece hash: %v", zap.Error(err))
			// }

			remotePieces = append(remotePieces, piece)
		}

		// we repair when the number of healthy files is less than or equal to the repair threshold
		// except for the case when the repair and success thresholds are the same (a case usually seen during testing)
		if int32(len(remotePieces)) <= remote.Redundancy.RepairThreshold && remote.Redundancy.RepairThreshold != remote.Redundancy.SuccessThreshold {
			return Error.New("Number of valid pieces is less than or equal to the repair threshold: %v < %v",
				len(remotePieces),
				remote.Redundancy.RepairThreshold,
			)
		}

		remote.RemotePieces = remotePieces
	}
	return nil
}

func (endpoint *Endpoint) validateBucket(bucket []byte) error {
	if len(bucket) == 0 {
		return errs.New("bucket not specified")
	}
	if bytes.ContainsAny(bucket, "/") {
		return errs.New("bucket should not contain slash")
	}
	return nil
}

func (endpoint *Endpoint) validateCommit(req *pb.SegmentCommitRequest) error {
	err := endpoint.validatePointer(req.Pointer)
	if err != nil {
		return err
	}

	if req.Pointer.Type == pb.Pointer_REMOTE {
		remote := req.Pointer.Remote

		if len(req.OriginalLimits) == 0 {
			return Error.New("no order limits")
		}
		if int32(len(req.OriginalLimits)) != remote.Redundancy.Total {
			return Error.New("invalid no order limit for piece")
		}

		for _, piece := range remote.RemotePieces {
			limit := req.OriginalLimits[piece.PieceNum]

			err := endpoint.orders.VerifyOrderLimitSignature(limit)
			if err != nil {
				return err
			}

			if limit == nil {
				return Error.New("invalid no order limit for piece")
			}
			derivedPieceID := remote.RootPieceId.Derive(piece.NodeId)
			if limit.PieceId.IsZero() || limit.PieceId != derivedPieceID {
				return Error.New("invalid order limit piece id")
			}
			if bytes.Compare(piece.NodeId.Bytes(), limit.StorageNodeId.Bytes()) != 0 {
				return Error.New("piece NodeID != order limit NodeID")
			}
		}
	}
	return nil
}

func (endpoint *Endpoint) validatePointer(pointer *pb.Pointer) error {
	if pointer == nil {
		return Error.New("no pointer specified")
	}

	// TODO does it all?
	if pointer.Type == pb.Pointer_REMOTE {
		if pointer.Remote == nil {
			return Error.New("no remote segment specified")
		}
		if pointer.Remote.RemotePieces == nil {
			return Error.New("no remote segment pieces specified")
		}
		if pointer.Remote.Redundancy == nil {
			return Error.New("no redundancy scheme specified")
		}
	}
	return nil
}

// CreatePath will create a Segment path
func CreatePath(projectID uuid.UUID, segmentIndex int64, bucket, path []byte) (storj.Path, error) {
	if segmentIndex < -1 {
		return "", errors.New("invalid segment index")
	}
	segment := "l"
	if segmentIndex > -1 {
		segment = "s" + strconv.FormatInt(segmentIndex, 10)
	}

	entries := make([]string, 0)
	entries = append(entries, projectID.String())
	entries = append(entries, segment)
	if len(bucket) != 0 {
		entries = append(entries, string(bucket))
	}
	if len(path) != 0 {
		entries = append(entries, string(path))
	}
	return storj.JoinPaths(entries...), nil
}

func (endpoint *Endpoint) validateRedundancy(redundancy *pb.RedundancyScheme) error {
	// TODO more validation, use validation from eestream.NewRedundancyStrategy
	if redundancy.ErasureShareSize <= 0 {
		return Error.New("erasure share size cannot be less than 0")
	}
	return nil
}
