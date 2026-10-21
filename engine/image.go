package engine

import (
	"bytes"
	"image"
	"io"
	"net/http"

	"github.com/metatube-community/metatube-sdk-go/common/number"
	R "github.com/metatube-community/metatube-sdk-go/constant"
	"github.com/metatube-community/metatube-sdk-go/detector"
	"github.com/metatube-community/metatube-sdk-go/engine/providerid"
	"github.com/metatube-community/metatube-sdk-go/imageutil"
	"github.com/metatube-community/metatube-sdk-go/model"
	mt "github.com/metatube-community/metatube-sdk-go/provider"
)

// Default position constants for different kind of images.
const (
	defaultActorPrimaryImagePosition  = 0.5
	defaultMoviePrimaryImagePosition  = 1.0
	defaultMovieThumbImagePosition    = 0.5
	defaultMovieBackdropImagePosition = 0.0
)

func (e *Engine) GetActorPrimaryImage(pid providerid.ProviderID) (image.Image, error) {
	info, err := e.GetActorInfoByProviderID(pid, true)
	if err != nil {
		return nil, err
	}
	if len(info.Images) == 0 {
		return nil, mt.ErrImageNotFound
	}
	return e.GetImageByURL(
		e.MustGetActorProviderByName(pid.Provider), info.Images[0],
		R.PrimaryImageRatio, defaultActorPrimaryImagePosition, false,
	)
}

func (e *Engine) GetMoviePrimaryImage(pid providerid.ProviderID, ratio, pos float64) (image.Image, error) {
	url, info, err := e.getPreferredMovieImageURLAndInfo(pid, true)
	if err != nil {
		return nil, err
	}
	if ratio < 0 /* default primary ratio */ {
		ratio = R.PrimaryImageRatio
	}
	var auto bool
	if pos < 0 /* manual position disabled */ {
		pos = defaultMoviePrimaryImagePosition
		auto = number.RequiresFaceDetection(info.Number)
	}
	return e.GetImageByURL(
		e.MustGetMovieProviderByName(pid.Provider),
		url, ratio, pos, auto,
	)
}

func (e *Engine) GetMovieThumbImage(pid providerid.ProviderID) (image.Image, error) {
	url, _, err := e.getPreferredMovieImageURLAndInfo(pid, false)
	if err != nil {
		return nil, err
	}
	return e.GetImageByURL(
		e.MustGetMovieProviderByName(pid.Provider), url,
		R.ThumbImageRatio, defaultMovieThumbImagePosition, false,
	)
}

func (e *Engine) GetMovieBackdropImage(pid providerid.ProviderID) (image.Image, error) {
	url, _, err := e.getPreferredMovieImageURLAndInfo(pid, false)
	if err != nil {
		return nil, err
	}
	return e.GetImageByURL(
		e.MustGetMovieProviderByName(pid.Provider), url,
		R.BackdropImageRatio, defaultMovieBackdropImagePosition, false,
	)
}

// ClearImageCache 清空本地原图缓存，返回删除的文件数。
// 缓存未启用时安全返回 (0, nil)。
func (e *Engine) ClearImageCache() (int, error) {
	return e.imageCache.Clear()
}

func (e *Engine) GetImageByURL(provider mt.Provider, url string, ratio, pos float64, auto bool) (img image.Image, err error) {
	if img, err = e.getImageByURL(provider, url); err != nil {
		return
	}
	if auto {
		// only turn on advanced for movie providers.
		advancedMode := e.IsMovieProvider(provider.Name())
		axisR, found := detector.FindPrimaryFaceAxisRatio(img, ratio, advancedMode)
		if found {
			pos = axisR // override the default position with detected position.
		}
	}
	return imageutil.CropImagePosition(img, ratio, pos), nil
}

func (e *Engine) getImageByURL(provider mt.Provider, url string) (img image.Image, err error) {
	// 先看本地缓存：命中就完全不碰网络。
	// 这是"代理挂了也能出图"的关键 —— 原图已经在磁盘上，
	// 叠角标、裁剪都是纯本地计算。
	if data, ok := e.imageCache.Get(url); ok {
		if img, _, decodeErr := imageutil.Decode(bytes.NewReader(data)); decodeErr == nil {
			return img, nil
		}
		// 缓存文件坏了（半截、格式变了）：当作未命中，继续走网络
	}

	resp, err := e.Fetch(url, provider)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if img, _, err = imageutil.Decode(bytes.NewReader(data)); err != nil {
		return nil, err
	}
	// 只缓存正常响应，避免把错误页当图片存下来
	if resp.StatusCode == http.StatusOK {
		e.imageCache.Put(url, data)
	}
	return img, nil
}

func (e *Engine) getPreferredMovieImageURLAndInfo(pid providerid.ProviderID, thumb bool) (url string, info *model.MovieInfo, err error) {
	info, err = e.GetMovieInfoByProviderID(pid, true)
	if err != nil {
		return
	}
	url = info.CoverURL
	if thumb && info.BigThumbURL != "" /* big thumb > cover */ {
		url = info.BigThumbURL
	} else if !thumb && info.BigCoverURL != "" /* big cover > cover */ {
		url = info.BigCoverURL
	}
	return
}
