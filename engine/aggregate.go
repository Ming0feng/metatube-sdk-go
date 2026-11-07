package engine

import (
	"reflect"
	"sort"
	"sync"
	"time"

	"gorm.io/datatypes"

	"github.com/metatube-community/metatube-sdk-go/model"
	mt "github.com/metatube-community/metatube-sdk-go/provider"
)

// FieldPriority maps a MovieInfo field name to an ordered list of provider
// names, mirroring amane's field_priority concept: for a given field the first
// non-empty value along the chain wins. Fields not listed fall back to the
// provider search priority (i.e. the order returned by SearchMovieAll).
//
// The keys are the Go struct field names of model.MovieInfo (e.g. "Title",
// "Summary", "CoverURL", "Genres"). Example:
//
//	map[string][]string{
//	    "Title":   {"JavBus", "FANZA"},
//	    "Summary": {"FANZA", "JavBus"},
//	    "CoverURL": {"DMM", "JavBus"},
//	}
type FieldPriority map[string][]string

// aggregateFields lists the fields that may be merged per-field. Identity
// fields (ID/Number/Provider/Homepage) are always taken from the
// highest-priority provider and are intentionally not merged.
var aggregateFields = []string{
	"Title", "Summary", "Director", "Actors",
	"ThumbURL", "BigThumbURL", "CoverURL", "BigCoverURL",
	"PreviewVideoURL", "PreviewVideoHLSURL", "PreviewImages",
	"Maker", "Label", "Series", "Genres",
	"Score", "Runtime", "ReleaseDate",
}

// movieCacheMaxAge 是聚合走 DB 缓存的窗口：该番号的最新详情若在窗口内，
// 就直接复用缓存聚合，跳过全源网络搜索，从而加速重复刮削。
const movieCacheMaxAge = 7 * 24 * time.Hour

// AggregateMovie searches the keyword across all providers and merges the
// results field-by-field instead of taking the first hit whole. The identity
// (ID/Number/Provider/Homepage) comes from the best-matching provider while
// every other field is filled from the first provider that returns it, so a
// title can come from JavBus while the plot comes from FANZA.
//
// fieldPriority may be nil; then the provider search priority is used for every
// field.
func (e *Engine) AggregateMovie(keyword string, fieldPriority FieldPriority) (*model.MovieInfo, error) {
	if keyword = normalizeKeyword(keyword); keyword == "" {
		return nil, mt.ErrInvalidKeyword
	}

	// DB 缓存优先：该番号已有新鲜详情时直接聚合，跳过全源网络搜索。
	if infos, err := e.getCachedMovieInfos(keyword, movieCacheMaxAge); err == nil && len(infos) > 0 {
		if ordered := e.sortMovieInfosByPriority(infos); len(ordered) > 0 {
			e.logger.Printf("Aggregate movie %s from cache: %d providers", keyword, len(ordered))
			return mergeMovieInfos(ordered, fieldPriority), nil
		}
	}

	results, err := e.SearchMovieAll(keyword, true)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, mt.ErrInfoNotFound
	}

	// Keep the best result per provider, preserving the search priority order.
	byProvider := make(map[string]*model.MovieSearchResult, len(results))
	order := make([]string, 0, len(results))
	for _, r := range results {
		if _, ok := byProvider[r.Provider]; !ok {
			byProvider[r.Provider] = r
			order = append(order, r.Provider)
		}
	}

	// Fetch the full info for every distinct provider concurrently.
	var (
		mu    sync.Mutex
		wg    sync.WaitGroup
		infos = make(map[string]*model.MovieInfo, len(order))
	)
	for _, name := range order {
		result := byProvider[name]
		wg.Add(1)
		go func(name string, result *model.MovieSearchResult) {
			defer wg.Done()
			provider, err := e.GetMovieProviderByName(name)
			if err != nil {
				return
			}
			info, err := e.getMovieInfoByProviderID(provider, result.ID, true /* lazy */)
			if err != nil || info == nil || !info.IsValid() {
				return
			}
			mu.Lock()
			infos[name] = info
			mu.Unlock()
		}(name, result)
	}
	wg.Wait()

	ordered := make([]*model.MovieInfo, 0, len(order))
	for _, name := range order {
		if info := infos[name]; info != nil {
			ordered = append(ordered, info)
		}
	}
	if len(ordered) == 0 {
		return nil, mt.ErrInfoNotFound
	}

	return mergeMovieInfos(ordered, fieldPriority), nil
}

// getCachedMovieInfos 从 DB 读取某番号在缓存窗口内的完整详情（按 number 或 id
// 精确匹配），供聚合复用缓存、跳过全源网络搜索。
func (e *Engine) getCachedMovieInfos(keyword string, maxAge time.Duration) ([]*model.MovieInfo, error) {
	var infos []*model.MovieInfo
	numberOrID := e.db.
		Where("number = ? COLLATE NOCASE", keyword).
		Or("id = ? COLLATE NOCASE", keyword)
	err := e.db.
		Where(numberOrID).
		Where("updated_at > ?", time.Now().Add(-maxAge)).
		Find(&infos).Error
	return infos, err
}

// sortMovieInfosByPriority 过滤无效详情及已下线 provider，并按 provider 优先级
// 降序排序（身份字段取排序后第一个）。
func (e *Engine) sortMovieInfosByPriority(infos []*model.MovieInfo) []*model.MovieInfo {
	ordered := make([]*model.MovieInfo, 0, len(infos))
	for _, info := range infos {
		if info == nil || !info.IsValid() {
			continue
		}
		if _, err := e.GetMovieProviderByName(info.Provider); err != nil {
			continue // provider 已下线，跳过
		}
		ordered = append(ordered, info)
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		pi, _ := e.GetMovieProviderByName(ordered[i].Provider)
		pj, _ := e.GetMovieProviderByName(ordered[j].Provider)
		return pi.Priority() > pj.Priority()
	})
	return ordered
}

// mergeMovieInfos merges ordered infos field-by-field. ordered is expected to
// be sorted by provider priority (the identity of ordered[0] wins).
func mergeMovieInfos(ordered []*model.MovieInfo, fp FieldPriority) *model.MovieInfo {
	primary := ordered[0]
	out := &model.MovieInfo{
		ID:       primary.ID,
		Number:   primary.Number,
		Provider: primary.Provider,
		Homepage: primary.Homepage,
	}
	for _, field := range aggregateFields {
		if v, ok := firstNonEmptyValue(field, chainForField(field, ordered, fp)); ok {
			setField(out, field, v)
		}
	}
	return out
}

// chainForField reorders the infos for a single field: providers listed in
// fp[field] move to the front (in that order), the rest keep their original
// relative order. This is the Go port of amane's compile_priority/chain_for.
func chainForField(field string, ordered []*model.MovieInfo, fp FieldPriority) []*model.MovieInfo {
	prefer := fp[field]
	if len(prefer) == 0 {
		return ordered
	}
	index := make(map[string]int, len(ordered))
	for i, info := range ordered {
		index[info.Provider] = i
	}
	out := make([]*model.MovieInfo, 0, len(ordered))
	seen := make(map[int]bool, len(ordered))
	for _, name := range prefer {
		if i, ok := index[name]; ok && !seen[i] {
			out = append(out, ordered[i])
			seen[i] = true
		}
	}
	for i, info := range ordered {
		if !seen[i] {
			out = append(out, info)
		}
	}
	return out
}

// firstNonEmptyValue returns the first non-empty value of field across infos.
func firstNonEmptyValue(field string, infos []*model.MovieInfo) (any, bool) {
	for _, info := range infos {
		if info == nil {
			continue
		}
		v := reflect.ValueOf(info).Elem().FieldByName(field)
		if v.IsValid() && isNonEmpty(v) {
			return v.Interface(), true
		}
	}
	return nil, false
}

// setField writes value back into the named field of out.
func setField(out *model.MovieInfo, field string, value any) {
	reflect.ValueOf(out).Elem().FieldByName(field).Set(reflect.ValueOf(value))
}

// isNonEmpty reports whether a reflect value is considered present. Zero
// values (empty string/slice, 0 number, zero time) are treated as empty.
func isNonEmpty(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.String:
		return v.String() != ""
	case reflect.Slice, reflect.Array:
		return v.Len() > 0
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() != 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint() != 0
	case reflect.Float32, reflect.Float64:
		return v.Float() != 0
	case reflect.Struct:
		if d, ok := v.Interface().(datatypes.Date); ok {
			return !time.Time(d).IsZero()
		}
		if t, ok := v.Interface().(time.Time); ok {
			return !t.IsZero()
		}
	}
	return false
}
