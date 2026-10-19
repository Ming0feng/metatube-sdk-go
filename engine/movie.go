package engine

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm/clause"

	"github.com/metatube-community/metatube-sdk-go/collection/sets"
	"github.com/metatube-community/metatube-sdk-go/collection/slices"
	"github.com/metatube-community/metatube-sdk-go/common/comparer"
	"github.com/metatube-community/metatube-sdk-go/common/number"
	"github.com/metatube-community/metatube-sdk-go/engine/providerid"
	"github.com/metatube-community/metatube-sdk-go/model"
	mt "github.com/metatube-community/metatube-sdk-go/provider"
)

func (e *Engine) searchMovieFromDB(keyword string, provider mt.MovieProvider, all bool) (results []*model.MovieSearchResult, err error) {
	var infos []*model.MovieInfo
	tx := e.db.
		// Note: keyword might be an ID or just a regular number, so we should
		// query both of them for best match. Also, case should not matter.
		Where("number = ? COLLATE NOCASE", keyword).
		Or("id = ? COLLATE NOCASE", keyword)
	if all {
		err = tx.Find(&infos).Error
	} else {
		err = e.db.
			Where("provider = ?", provider.Name()).
			Where(tx).
			Find(&infos).Error
	}
	if err == nil {
		for _, info := range infos {
			if !info.IsValid() {
				// normally it is valid, but just in case.
				continue
			}
			results = append(results, info.ToSearchResult())
		}
	}
	return
}

func (e *Engine) searchMovie(keyword string, provider mt.MovieProvider, fallback bool) (results []*model.MovieSearchResult, err error) {
	// Regular keyword searching.
	if searcher, ok := provider.(mt.MovieSearcher); ok {
		if keyword = searcher.NormalizeMovieKeyword(keyword); keyword == "" {
			return nil, mt.ErrInvalidKeyword
		}
		if fallback {
			defer func() {
				if innerResults, innerErr := e.searchMovieFromDB(keyword, provider, false);
				// ignore DB query error.
				innerErr == nil && len(innerResults) > 0 {
					// overwrite error.
					err = nil
					// update results.
					msr := sets.NewOrderedSetWithHash(func(v *model.MovieSearchResult) string { return v.Provider + v.ID })
					msr.Add(results...)
					msr.Add(innerResults...)
					results = msr.AsSlice()
				}
			}()
		}
		return searcher.SearchMovie(keyword)
	}
	// Fallback to movie info querying.
	info, err := e.getMovieInfoByProviderID(provider, keyword, true)
	if err != nil {
		return nil, err
	}
	return []*model.MovieSearchResult{info.ToSearchResult()}, nil
}

func (e *Engine) SearchMovie(keyword, name string, fallback bool) ([]*model.MovieSearchResult, error) {
	if keyword = normalizeKeyword(keyword); keyword == "" {
		return nil, mt.ErrInvalidKeyword
	}
	provider, err := e.GetMovieProviderByName(name)
	if err != nil {
		return nil, err
	}
	return e.searchMovie(keyword, provider, fallback)
}

func (e *Engine) searchMovieAll(keyword string) (results []*model.MovieSearchResult, err error) {
	type response struct {
		Results   []*model.MovieSearchResult
		Error     error
		Provider  mt.MovieProvider
		StartTime time.Time
		EndTime   time.Time
	}
	respCh := make(chan response)

	var wg sync.WaitGroup
	for _, provider := range e.movieProviders.Iterator() {
		wg.Add(1)
		// Goroutine started time.
		startTime := time.Now()
		// Async searching.
		go func(provider mt.MovieProvider) {
			defer wg.Done()
			innerResults, innerErr := e.searchMovie(keyword, provider, false)
			respCh <- response{
				Results:   innerResults,
				Error:     innerErr,
				Provider:  provider,
				StartTime: startTime,
				EndTime:   time.Now(),
			}
		}(provider)
	}
	go func() {
		wg.Wait()
		// notify when all searching tasks done.
		close(respCh)
	}()

	ds := make([]string, 0, e.movieProviders.Len())
	// response channel.
	for resp := range respCh {
		ds = append(ds, func(a, b, c any) string {
			if c == nil {
				c = "no error"
			}
			return fmt.Sprintf("%s(%s):<%v>", a, b, c)
		}(
			resp.Provider.Name(),
			resp.EndTime.Sub(resp.StartTime),
			resp.Error,
		))

		if resp.Error != nil {
			continue
		}
		results = append(results, resp.Results...)
	}

	e.logger.Printf("Search keyword %s: %s", keyword, strings.Join(ds, " | "))
	return
}

// normalizeKeyword cleans up a movie keyword before searching. Whitespace is
// removed first because file names often carry it inside the number, e.g.
// "MIA B-585" should be searched as "MIAB-585".
func normalizeKeyword(keyword string) string {
	return number.Trim(strings.Join(strings.Fields(keyword), ""))
}

// padNumber pads the trailing numeric segment to 3 digits, which is the
// canonical JAV numbering convention (e.g. "DSAM-13" -> "DSAM-013").
func padNumber(s string) string {
	if ss := regexp.MustCompile(`(?i)^(.*?)(\d+)$`).FindStringSubmatch(s); len(ss) == 3 && len(ss[2]) < 3 {
		return ss[1] + fmt.Sprintf("%03s", ss[2])
	}
	return s
}

// SearchMovieAll searches the keyword from all providers.
func (e *Engine) SearchMovieAll(keyword string, fallback bool) (results []*model.MovieSearchResult, err error) {
	if keyword = normalizeKeyword(keyword); keyword == "" {
		return nil, mt.ErrInvalidKeyword
	}

	defer func() {
		if err != nil {
			return
		}
		if len(results) == 0 {
			err = mt.ErrInfoNotFound
			return
		}
		// remove duplicate results, if any.
		msr := sets.NewOrderedSetWithHash(func(v *model.MovieSearchResult) string { return v.Provider + v.ID })
		msr.Add(results...)
		results = msr.AsSlice()
		// post-processing
		ps := new(slices.WeightedSlice[*model.MovieSearchResult, float64])
		for _, result := range results {
			if !result.IsValid() /* validation check */ {
				continue
			}
			if _, err := e.GetMovieProviderByName(result.Provider); err != nil {
				e.logger.Printf("ignore provider %s as not found", result.Provider)
				continue
			}
			priority := comparer.Compare(keyword, result.Number) *
				e.MustGetMovieProviderByName(result.Provider).Priority()
			ps.Append(result, priority)
		}
		// sort by priority.
		results = ps.SortFunc(sort.Stable).Slice()
	}()

	if fallback /* query database for missing results  */ {
		defer func() {
			if innerResults, innerErr := e.searchMovieFromDB(keyword, nil, true);
			// ignore DB query error.
			innerErr == nil && len(innerResults) > 0 {
				// overwrite error.
				err = nil
				// append results.
				results = append(results, innerResults...)
			}
		}()
	}

	results, err = e.searchMovieAll(keyword)
	return
}

func (e *Engine) getMovieInfoFromDB(provider mt.MovieProvider, id string) (*model.MovieInfo, error) {
	info := &model.MovieInfo{}
	err := e.db. // Exact match here.
			Where("provider = ?", provider.Name()).
			Where("id = ? COLLATE NOCASE", id).
			First(info).Error
	return info, err
}

func (e *Engine) getMovieInfoWithCallback(provider mt.MovieProvider, id string, lazy bool, callback func() (*model.MovieInfo, error)) (info *model.MovieInfo, err error) {
	defer func() {
		// metadata validation check.
		if err == nil && (info == nil || !info.IsValid()) {
			err = mt.ErrIncompleteMetadata
		}
	}()
	// Query DB first (by id).
	if lazy {
		if info, err = e.getMovieInfoFromDB(provider, id); err == nil && info.IsValid() {
			return // ignore DB query error.
		}
	}
	// delayed info auto-save.
	defer func() {
		if err == nil && info.IsValid() {
			e.db.Clauses(clause.OnConflict{
				UpdateAll: true,
			}).Create(info) // ignore error
		}
	}()
	return callback()
}

func (e *Engine) getMovieInfoByProviderID(provider mt.MovieProvider, id string, lazy bool) (*model.MovieInfo, error) {
	if id = provider.NormalizeMovieID(id); id == "" {
		return nil, mt.ErrInvalidID
	}
	return e.getMovieInfoWithCallback(provider, id, lazy, func() (*model.MovieInfo, error) {
		return provider.GetMovieInfoByID(id)
	})
}

func (e *Engine) GetMovieInfoByProviderID(pid providerid.ProviderID, lazy bool) (*model.MovieInfo, error) {
	provider, err := e.GetMovieProviderByName(pid.Provider)
	if err != nil {
		// The provider may have been retired (e.g. JAV321). Rather than
		// failing or serving stale data, discard the cached record and
		// scrape the movie again from scratch using its number.
		if info, reErr := e.rescrapeMovieInfoByID(pid); reErr == nil {
			return info, nil
		}
		return nil, err
	}
	return e.getMovieInfoByProviderID(provider, pid.ID, lazy)
}

// rescrapeMovieInfoByID discards the metadata cached under a retired provider
// and scrapes the movie again by its number. The returned info belongs to
// whichever provider actually matched, so clients migrate to a new provider id.
func (e *Engine) rescrapeMovieInfoByID(pid providerid.ProviderID) (*model.MovieInfo, error) {
	// Drop previous save.
	if err := e.db.
		Where("provider = ?", pid.Provider).
		Where("id = ?", pid.ID).
		Delete(&model.MovieInfo{}).Error; err != nil {
		return nil, err
	}

	keyword := strings.ToUpper(strings.TrimSpace(pid.ID))
	if keyword == "" {
		return nil, mt.ErrInvalidID
	}

	// Retired providers stored numbers without leading zeros (e.g. "dsam-13"),
	// while the canonical form is zero-padded ("DSAM-013"). Try the padded
	// form first, otherwise the search may hit another title by collision.
	var results []*model.MovieSearchResult
	var searchErr error
	for _, term := range []string{padNumber(keyword), keyword} {
		if results, searchErr = e.SearchMovieAll(term, false); searchErr == nil && len(results) > 0 {
			keyword = term
			break
		}
	}
	if len(results) == 0 {
		if searchErr != nil {
			return nil, searchErr
		}
		return nil, mt.ErrInfoNotFound
	}

	// Prefer an exact number match.
	best := results[0]
	for _, result := range results {
		if strings.EqualFold(result.Number, keyword) {
			best = result
			break
		}
	}

	provider, err := e.GetMovieProviderByName(best.Provider)
	if err != nil {
		return nil, err
	}
	return e.getMovieInfoByProviderID(provider, best.ID, false /* always fresh */)
}

func (e *Engine) getMovieInfoByProviderURL(provider mt.MovieProvider, rawURL string, lazy bool) (*model.MovieInfo, error) {
	id, err := provider.ParseMovieIDFromURL(rawURL)
	switch {
	case err != nil:
		return nil, err
	case id == "":
		return nil, mt.ErrInvalidURL
	}
	return e.getMovieInfoWithCallback(provider, id, lazy, func() (*model.MovieInfo, error) {
		return provider.GetMovieInfoByURL(rawURL)
	})
}

func (e *Engine) GetMovieInfoByURL(rawURL string, lazy bool) (*model.MovieInfo, error) {
	provider, err := e.GetMovieProviderByURL(rawURL)
	if err != nil {
		return nil, err
	}
	return e.getMovieInfoByProviderURL(provider, rawURL, lazy)
}
