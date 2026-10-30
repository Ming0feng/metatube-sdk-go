package engine

import (
	"sync"

	"github.com/longbridgeapp/opencc"
	"golang.org/x/text/language"

	"github.com/metatube-community/metatube-sdk-go/model"
	mt "github.com/metatube-community/metatube-sdk-go/provider"
)

var (
	t2sOnce sync.Once
	t2sConv *opencc.OpenCC
)

// simplifyMovieInfo 对中文源的元数据做繁体→简体转换。中文片源（如 AiRav
// 简体版、IQQTV）的数据仍会混入繁体字，这里统一收敛为简体。
func simplifyMovieInfo(provider mt.Provider, info *model.MovieInfo) {
	if info == nil || provider.Language() != language.Chinese {
		return
	}
	info.Title = convertToSimplified(info.Title)
	info.Summary = convertToSimplified(info.Summary)
	info.Director = convertToSimplified(info.Director)
	info.Maker = convertToSimplified(info.Maker)
	info.Label = convertToSimplified(info.Label)
	info.Series = convertToSimplified(info.Series)
	info.Actors = convertSliceToSimplified(info.Actors)
	info.Genres = convertSliceToSimplified(info.Genres)
}

// convertToSimplified 做繁体→简体转换，失败时原样返回。OpenCC 的 t2s 转换
// 对简体/日文等非繁体文本基本是幂等的，因此可安全复用。
func convertToSimplified(s string) string {
	if s == "" {
		return s
	}
	t2sOnce.Do(func() {
		t2sConv, _ = opencc.New("t2s")
	})
	if t2sConv == nil {
		return s
	}
	if out, err := t2sConv.Convert(s); err == nil {
		return out
	}
	return s
}

func convertSliceToSimplified(in []string) []string {
	if len(in) == 0 {
		return in
	}
	out := make([]string, 0, len(in))
	for _, s := range in {
		out = append(out, convertToSimplified(s))
	}
	return out
}
