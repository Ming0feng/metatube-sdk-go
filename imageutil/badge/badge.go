package badge

import (
	"fmt"
	"image"
	"strings"
	"time"

	"github.com/jellydator/ttlcache/v3"

	"github.com/metatube-community/metatube-sdk-go/common/fetch"
	"github.com/metatube-community/metatube-sdk-go/imageutil"
)

var (
	badgeCache = ttlcache.New[string, image.Image](
		ttlcache.WithTTL[string, image.Image](30*time.Minute),
		ttlcache.WithCapacity[string, image.Image](10),
	)
	badgeFetcher = fetch.Default(nil)
)

func init() {
	// start badge cache.
	go badgeCache.Start()
}

// Position is the anchor of a badge on the source image.
type Position string

const (
	// TopLeft anchors the badge at the top-left corner (default).
	TopLeft Position = "tl"
	// TopRight anchors the badge at the top-right corner.
	TopRight Position = "tr"
)

// Badge applies one or more badges to src.
//
// The spec is a comma-separated list of badges, each of which may carry an
// optional position prefix, for example:
//
//	"zimu.png"                          -> top-left zimu.png
//	"tr:uncensored.png"                 -> top-right uncensored.png
//	"zimu.png,tr:uncensored.png"        -> both corners
func Badge(src image.Image, spec string) (image.Image, error) {
	for _, entry := range strings.Split(spec, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		position, name := TopLeft, entry
		if i := strings.Index(entry, ":"); i > 0 {
			if pos := Position(strings.ToLower(strings.TrimSpace(entry[:i]))); pos == TopLeft || pos == TopRight {
				position = pos
				name = strings.TrimSpace(entry[i+1:])
			}
		}
		if name == "" {
			continue
		}

		img, err := resolve(name)
		if err != nil {
			return nil, err
		}

		wmk := imageutil.Resize(img, 0, src.Bounds().Dy()/5 /* 0.2 */)
		pt := image.Point{}
		if position == TopRight {
			pt = image.Point{X: src.Bounds().Dx() - wmk.Bounds().Dx()}
		}
		src = imageutil.Watermark(src, wmk, pt)
	}
	return src, nil
}

func resolve(name string) (image.Image, error) {
	if item := badgeCache.Get(name); item != nil {
		return item.Value(), nil
	}
	resp, err := badgeFetcher.Fetch(name)
	if err != nil {
		return nil, fmt.Errorf("fetch badge: %w", err)
	}
	defer resp.Body.Close()
	// decode badge image.
	img, _, err := imageutil.Decode(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("decode badge: %w", err)
	}
	badgeCache.Set(name, img, ttlcache.DefaultTTL)
	return img, nil
}
