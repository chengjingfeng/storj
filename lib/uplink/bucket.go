// Copyright (C) 2019 Storj Labs, Inc.
// See LICENSE for copying information.

package uplink

import (
	"context"
	"io"
	"time"

	"github.com/zeebo/errs"

	"storj.io/storj/pkg/metainfo/kvmetainfo"
	"storj.io/storj/pkg/storage/streams"
	"storj.io/storj/pkg/storj"
	"storj.io/storj/pkg/stream"
)

// Bucket represents operations you can perform on a bucket
type Bucket struct {
	BucketConfig
	Name    string
	Created time.Time

	bucket   storj.Bucket
	metainfo *kvmetainfo.DB
	streams  streams.Store
}

// OpenObject returns an Object handle, if authorized.
func (b *Bucket) OpenObject(ctx context.Context, path storj.Path) (o *Object, err error) {
	defer mon.Task()(&ctx)(&err)

	info, err := b.metainfo.GetObject(ctx, b.Name, path)
	if err != nil {
		return nil, err
	}

	return &Object{
		Meta: ObjectMeta{
			Bucket:      info.Bucket.Name,
			Path:        info.Path,
			IsPrefix:    info.IsPrefix,
			ContentType: info.ContentType,
			Metadata:    info.Metadata,
			Created:     info.Created,
			Modified:    info.Modified,
			Expires:     info.Expires,
			Size:        info.Size,
			Checksum:    info.Checksum,
			Volatile: struct {
				EncryptionParameters storj.EncryptionParameters
				RedundancyScheme     storj.RedundancyScheme
				SegmentsSize         int64
			}{
				EncryptionParameters: info.ToEncryptionParameters(),
				RedundancyScheme:     info.RedundancyScheme,
				SegmentsSize:         info.FixedSegmentSize,
			},
		},
		metainfoDB: b.metainfo,
		streams:    b.streams,
	}, nil
}

// UploadOptions controls options about uploading a new Object, if authorized.
type UploadOptions struct {
	// ContentType, if set, gives a MIME content-type for the Object.
	ContentType string
	// Metadata contains additional information about an Object. It can
	// hold arbitrary textual fields and can be retrieved together with the
	// Object. Field names can be at most 1024 bytes long. Field values are
	// not individually limited in size, but the total of all metadata
	// (fields and values) can not exceed 4 kiB.
	Metadata map[string]string
	// Expires is the time at which the new Object can expire (be deleted
	// automatically from storage nodes).
	Expires time.Time

	// Volatile groups config values that are likely to change semantics
	// or go away entirely between releases. Be careful when using them!
	Volatile struct {
		// EncryptionParameters determines the cipher suite to use for
		// the Object's data encryption. If not set, the Bucket's
		// defaults will be used.
		EncryptionParameters storj.EncryptionParameters

		// RedundancyScheme determines the Reed-Solomon and/or Forward
		// Error Correction encoding parameters to be used for this
		// Object.
		RedundancyScheme storj.RedundancyScheme
	}
}

// UploadObject uploads a new object, if authorized.
func (b *Bucket) UploadObject(ctx context.Context, path storj.Path, data io.Reader, opts *UploadOptions) (err error) {
	defer mon.Task()(&ctx)(&err)

	if opts == nil {
		opts = &UploadOptions{}
	}

	if opts.Volatile.RedundancyScheme.Algorithm == 0 {
		opts.Volatile.RedundancyScheme.Algorithm = b.Volatile.RedundancyScheme.Algorithm
	}
	if opts.Volatile.RedundancyScheme.OptimalShares == 0 {
		opts.Volatile.RedundancyScheme.OptimalShares = b.Volatile.RedundancyScheme.OptimalShares
	}
	if opts.Volatile.RedundancyScheme.RepairShares == 0 {
		opts.Volatile.RedundancyScheme.RepairShares = b.Volatile.RedundancyScheme.RepairShares
	}
	if opts.Volatile.RedundancyScheme.RequiredShares == 0 {
		opts.Volatile.RedundancyScheme.RequiredShares = b.Volatile.RedundancyScheme.RequiredShares
	}
	if opts.Volatile.RedundancyScheme.ShareSize == 0 {
		opts.Volatile.RedundancyScheme.ShareSize = b.Volatile.RedundancyScheme.ShareSize
	}
	if opts.Volatile.RedundancyScheme.TotalShares == 0 {
		opts.Volatile.RedundancyScheme.TotalShares = b.Volatile.RedundancyScheme.TotalShares
	}
	if opts.Volatile.EncryptionParameters.CipherSuite == storj.EncUnspecified {
		opts.Volatile.EncryptionParameters.CipherSuite = b.EncryptionParameters.CipherSuite
	}
	if opts.Volatile.EncryptionParameters.BlockSize == 0 {
		opts.Volatile.EncryptionParameters.BlockSize = b.EncryptionParameters.BlockSize
	}
	createInfo := storj.CreateObject{
		ContentType:      opts.ContentType,
		Metadata:         opts.Metadata,
		Expires:          opts.Expires,
		RedundancyScheme: opts.Volatile.RedundancyScheme,
		EncryptionScheme: opts.Volatile.EncryptionParameters.ToEncryptionScheme(),
	}

	obj, err := b.metainfo.CreateObject(ctx, b.Name, path, &createInfo)
	if err != nil {
		return err
	}

	mutableStream, err := obj.CreateStream(ctx)
	if err != nil {
		return err
	}

	upload := stream.NewUpload(ctx, mutableStream, b.streams)

	_, err = io.Copy(upload, data)

	return errs.Combine(err, upload.Close())
}

// DeleteObject removes an object, if authorized.
func (b *Bucket) DeleteObject(ctx context.Context, path storj.Path) (err error) {
	defer mon.Task()(&ctx)(&err)
	return b.metainfo.DeleteObject(ctx, b.bucket.Name, path)
}

// ListOptions controls options for the ListObjects() call.
type ListOptions = storj.ListOptions

// ListObjects lists objects a user is authorized to see.
func (b *Bucket) ListObjects(ctx context.Context, cfg *ListOptions) (list storj.ObjectList, err error) {
	defer mon.Task()(&ctx)(&err)
	if cfg == nil {
		cfg = &storj.ListOptions{}
	}
	return b.metainfo.ListObjects(ctx, b.bucket.Name, *cfg)
}

// Close closes the Bucket session.
func (b *Bucket) Close() error {
	return nil
}
