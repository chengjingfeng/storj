// Copyright (C) 2019 Storj Labs, Inc.
// See LICENSE for copying information.

package cmd

import (
	"fmt"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/spf13/cobra"
	"github.com/zeebo/errs"

	"storj.io/storj/internal/fpath"
	libuplink "storj.io/storj/lib/uplink"
	"storj.io/storj/pkg/cfgstruct"
	"storj.io/storj/pkg/encryption"
	"storj.io/storj/pkg/macaroon"
	"storj.io/storj/pkg/process"
)

var shareCfg struct {
	DisallowReads     bool     `default:"false" help:"if true, disallow reads"`
	DisallowWrites    bool     `default:"false" help:"if true, disallow writes"`
	DisallowLists     bool     `default:"false" help:"if true, disallow lists"`
	DisallowDeletes   bool     `default:"false" help:"if true, disallow deletes"`
	Readonly          bool     `default:"false" help:"implies disallow_writes and disallow_deletes"`
	Writeonly         bool     `default:"false" help:"implies disallow_reads and disallow_lists"`
	NotBefore         string   `help:"disallow access before this time"`
	NotAfter          string   `help:"disallow access after this time"`
	AllowedPathPrefix []string `help:"whitelist of bucket path prefixes to require"`
}

func init() {
	// sadly, we have to use addCmd so that it adds the cfg struct to the flags
	// so that we can open projects and buckets. that pulls in so many unnecessary
	// flags which makes figuring out the share command really hard. oh well.
	shareCmd := addCmd(&cobra.Command{
		Use:   "share",
		Short: "Creates a possibly restricted api key",
		RunE:  shareMain,
	}, RootCmd)

	cfgstruct.Bind(shareCmd.Flags(), &shareCfg)
}

const shareISO8601 = "2006-01-02T15:04:05-0700"

func parseHumanDate(date string, now time.Time) (*time.Time, error) {
	if date == "" {
		return nil, nil
	} else if date == "now" {
		return &now, nil
	} else if date[0] == '+' {
		d, err := time.ParseDuration(date[1:])
		t := now.Add(d)
		return &t, errs.Wrap(err)
	} else if date[0] == '-' {
		d, err := time.ParseDuration(date[1:])
		t := now.Add(-d)
		return &t, errs.Wrap(err)
	} else {
		t, err := time.Parse(shareISO8601, date)
		return &t, errs.Wrap(err)
	}
}

// shareMain is the function executed when shareCmd is called
func shareMain(cmd *cobra.Command, args []string) (err error) {
	ctx := process.Ctx(cmd)
	now := time.Now()

	notBefore, err := parseHumanDate(shareCfg.NotBefore, now)
	if err != nil {
		return err
	}
	notAfter, err := parseHumanDate(shareCfg.NotAfter, now)
	if err != nil {
		return err
	}

	// TODO(jeff): we have to have the server side of things expecting macaroons
	// before we can change libuplink to use macaroons because of all the tests.
	// For now, just use the raw macaroon library.

	key, err := macaroon.ParseAPIKey(cfg.Client.APIKey)
	if err != nil {
		return err
	}

	caveat, err := macaroon.NewCaveat()
	if err != nil {
		return err
	}

	caveat.DisallowDeletes = shareCfg.DisallowDeletes || shareCfg.Readonly
	caveat.DisallowLists = shareCfg.DisallowLists || shareCfg.Writeonly
	caveat.DisallowReads = shareCfg.DisallowReads || shareCfg.Writeonly
	caveat.DisallowWrites = shareCfg.DisallowWrites || shareCfg.Readonly
	caveat.NotBefore = notBefore
	caveat.NotAfter = notAfter

	var project *libuplink.Project
	var access libuplink.EncryptionAccess
	copy(access.Key[:], []byte(cfg.Enc.Key))
	cache := make(map[string]*libuplink.BucketConfig)

	for _, path := range shareCfg.AllowedPathPrefix {
		p, err := fpath.New(path)
		if err != nil {
			return err
		}
		if p.IsLocal() {
			return errs.New("required path must be remote: %q", path)
		}

		bi, ok := cache[p.Bucket()]
		if !ok {
			if project == nil {
				project, err = cfg.GetProject(ctx)
				if err != nil {
					return err
				}
				defer func() { err = errs.Combine(err, project.Close()) }()
			}

			_, bi, err = project.GetBucketInfo(ctx, p.Bucket())
			if err != nil {
				return err
			}

			cache[p.Bucket()] = bi
		}

		encPath, err := encryption.EncryptPath(path, bi.PathCipher.ToCipher(), &access.Key)
		if err != nil {
			return err
		}

		caveat.AllowedPaths = append(caveat.AllowedPaths, &macaroon.Caveat_Path{
			Bucket:              []byte(p.Bucket()),
			EncryptedPathPrefix: []byte(encPath),
		})
	}

	{
		// Times don't marshal very well with MarshalTextString, and the nonce doesn't
		// matter to humans, so handle those explicitly and then dispatch to the generic
		// routine to avoid having to print all the things individually.
		caveatCopy := proto.Clone(&caveat).(*macaroon.Caveat)
		caveatCopy.Nonce = nil
		if caveatCopy.NotBefore != nil {
			fmt.Println("not before:", caveatCopy.NotBefore.Truncate(0).Format(shareISO8601))
			caveatCopy.NotBefore = nil
		}
		if caveatCopy.NotAfter != nil {
			fmt.Println("not after:", caveatCopy.NotAfter.Truncate(0).Format(shareISO8601))
			caveatCopy.NotAfter = nil
		}
		fmt.Print(proto.MarshalTextString(caveatCopy))
	}

	key, err = key.Restrict(caveat)
	if err != nil {
		return err
	}

	fmt.Println("new key:", key.Serialize())
	return nil
}
