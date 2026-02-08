package blobfs

import (
	"context"
	"fmt"
	"io/fs"
	"net/url"

	"gocloud.dev/blob"
)

var _ blob.BucketURLOpener = (*s3WithRegionOpener)(nil)

// s3WithRegionOpener is a blob.BucketURLOpener that looks up the region in
// the IMDS filesystem if it's not set in the URL.
type s3WithRegionOpener struct {
	imdsfs fs.FS
	opener blob.BucketURLOpener
}

func (r *s3WithRegionOpener) OpenBucketURL(ctx context.Context, u *url.URL) (*blob.Bucket, error) {
	// first check if the region is set in the URL
	q := u.Query()
	if q.Get("region") == "" && r.imdsfs != nil {
		// if we have an IMDS filesystem, use it to get the region
		region, err := fs.ReadFile(r.imdsfs, "meta-data/placement/region")
		if err != nil {
			return nil, fmt.Errorf("couldn't get region from IMDS: %w", err)
		}

		q.Set("region", string(region))

		u.RawQuery = q.Encode()
	}

	return r.opener.OpenBucketURL(ctx, u)
}
