// Package avsox implements an uncensored information index backed by
// avsox.click. The site is a Vue SPA whose metadata is served by a Yii JSON
// API: fetch the csrf-token, then POST search / getMovie with a JSON array.
package avsox

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/antchfx/htmlquery"
	"golang.org/x/text/language"

	"github.com/metatube-community/metatube-sdk-go/common/fetch"
	"github.com/metatube-community/metatube-sdk-go/common/parser"
	"github.com/metatube-community/metatube-sdk-go/model"
	"github.com/metatube-community/metatube-sdk-go/provider"
	"github.com/metatube-community/metatube-sdk-go/provider/internal/scraper"
)

var (
	_ provider.MovieProvider = (*Avsox)(nil)
	_ provider.MovieSearcher = (*Avsox)(nil)
)

const (
	Name     = "AVSOX"
	Priority = 1000 - 20
)

const (
	baseURL = "https://avsox.click"
	apiURL  = "https://avsox.click/javu/data/api/%s"
)

var moviePathRe = regexp.MustCompile(`/(?:cn|ja|en|tw)?/?(?:movies|movie)/([A-Za-z0-9]+)`)

type Avsox struct {
	*scraper.Scraper
	fetcher *fetch.Fetcher
}

func New() *Avsox {
	return &Avsox{
		Scraper: scraper.NewDefaultScraper(Name, baseURL, Priority, language.Chinese),
		fetcher: fetch.Default(nil),
	}
}

func (av *Avsox) NormalizeMovieID(id string) string {
	return strings.TrimSpace(id)
}

func (av *Avsox) GetMovieInfoByID(id string) (info *model.MovieInfo, err error) {
	return av.GetMovieInfoByURL(fmt.Sprintf("%s/cn/movies/%s", baseURL, id))
}

func (av *Avsox) ParseMovieIDFromURL(rawURL string) (string, error) {
	if m := moviePathRe.FindStringSubmatch(rawURL); m != nil {
		return m[1], nil
	}
	return "", provider.ErrInvalidURL
}

func (av *Avsox) GetMovieInfoByURL(rawURL string) (info *model.MovieInfo, err error) {
	movieID, err := av.ParseMovieIDFromURL(rawURL)
	if err != nil {
		return nil, err
	}

	data, err := av.api("getMovie", []any{movieID, "cn"}, rawURL)
	if err != nil {
		return nil, err
	}
	movie, ok := data.(map[string]any)
	if !ok {
		return nil, provider.ErrInfoNotFound
	}

	number := getString(movie, "movieFanHao")
	if number == "" {
		return nil, provider.ErrInfoNotFound
	}

	info = &model.MovieInfo{
		ID:            number,
		Number:        number,
		Title:         firstString(movie["title"], movie["title_ja"], movie["title_en"], movie["title_cn"], movie["title_tw"]),
		Provider:      av.Name(),
		Homepage:      rawURL,
		Actors:        namedNames(movie["star"], "star"),
		Maker:         namedName(movie["studio"], "studio"),
		Label:         namedName(movie["label"], "label"),
		Series:        namedName(movie["series"], "series"),
		Director:      namedName(movie["director"], "director"),
		Genres:        namedNames(movie["genre"], "genre"),
		Summary:       firstString(movie["description"], movie["description_cn"], movie["description_ja"], movie["description_en"], movie["description_tw"]),
		PreviewImages: stringList(movie["sampleLarge"]),
	}

	info.ReleaseDate = parser.ParseDate(getString(movie, "releaseDate"))
	info.Runtime = getInt(movie, "length")

	info.CoverURL = av.absURL(getString(movie, "posterLarge"))
	if info.CoverURL == "" {
		info.CoverURL = av.absURL(getString(movie, "posterSmall"))
	}
	info.ThumbURL = info.CoverURL
	info.BigCoverURL = info.CoverURL

	return info, nil
}

func (av *Avsox) NormalizeMovieKeyword(keyword string) string {
	return strings.ToUpper(strings.Join(strings.Fields(keyword), ""))
}

func (av *Avsox) SearchMovie(keyword string) (results []*model.MovieSearchResult, err error) {
	referer := fmt.Sprintf("%s/cn/search/%s", baseURL, url.PathEscape(keyword))
	data, err := av.api("search", []any{map[string]any{"search": keyword, "lang": "cn"}, 60, 1}, referer)
	if err != nil {
		return nil, err
	}
	movies, ok := data.([]any)
	if !ok {
		return nil, nil
	}
	movieID := pickMovieID(movies, keyword)
	if movieID == "" {
		return nil, nil
	}

	homepage := fmt.Sprintf("%s/cn/movies/%s", baseURL, movieID)
	results = append(results, &model.MovieSearchResult{
		ID:       movieID,
		Number:   keyword,
		Title:    keyword,
		Provider: av.Name(),
		Homepage: homepage,
	})
	return results, nil
}

// api 完成 avsox 的 Yii JSON API 调用：GET 取 csrf-token，POST 方法体，解包 code=200 的 data。
func (av *Avsox) api(method string, payload []any, referer string) (any, error) {
	// 1. 取 csrf-token。
	resp, err := av.fetcher.Get(baseURL+"/cn/", fetch.WithRaiseForStatus(false))
	if err != nil {
		return nil, err
	}
	token := csrfToken(resp)
	resp.Body.Close()

	// 2. POST JSON。
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	resp, err = av.fetcher.Post(fmt.Sprintf(apiURL, method), bytes.NewReader(body),
		fetch.WithHeader("Content-Type", "application/json"),
		fetch.WithHeader("Accept", "application/json, text/plain, */*"),
		fetch.WithHeader("Origin", baseURL),
		fetch.WithHeader("Referer", referer),
		fetch.WithHeader("X-Requested-With", "XMLHttpRequest"),
		fetch.WithHeader("X-CSRF-Token", token),
	)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var envelope struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, err
	}
	if envelope.Code != 200 {
		return nil, provider.ErrInfoNotFound
	}
	var data any
	if err = json.Unmarshal(envelope.Data, &data); err != nil {
		return nil, err
	}
	return data, nil
}

func csrfToken(resp *http.Response) string {
	doc, err := htmlquery.Parse(resp.Body)
	if err != nil {
		return ""
	}
	node := htmlquery.FindOne(doc, `//meta[@name="csrf-token"]`)
	if node == nil {
		return ""
	}
	return strings.TrimSpace(htmlquery.SelectAttr(node, "content"))
}

func (av *Avsox) absURL(raw string) string {
	if raw == "" {
		return ""
	}
	if u, err := url.Parse(raw); err == nil {
		if u.IsAbs() {
			return u.String()
		}
	}
	return fmt.Sprintf("%s/%s", strings.TrimRight(baseURL, "/"), strings.TrimLeft(raw, "/"))
}

func pickMovieID(movies []any, number string) string {
	exact := strings.ToLower(number)
	folded := foldNumber(number)
	var foldedHit string
	for _, item := range movies {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		fanhao := getString(m, "movieFanHao")
		movieID := getString(m, "movieId")
		if fanhao == "" || movieID == "" {
			continue
		}
		if strings.ToLower(fanhao) == exact {
			return movieID
		}
		if foldedHit == "" && foldNumber(fanhao) == folded {
			foldedHit = movieID
		}
	}
	return foldedHit
}

func getString(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func getInt(m map[string]any, key string) int {
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return 0
}

func firstString(values ...any) string {
	for _, v := range values {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

// namedName 从命名实体（可能为对象或单元素数组）中取名字。
func namedName(v any, prefix string) string {
	m, ok := v.(map[string]any)
	if !ok {
		if arr, ok := v.([]any); ok && len(arr) > 0 {
			m, _ = arr[0].(map[string]any)
		}
	}
	if m == nil {
		return ""
	}
	for _, suffix := range []string{"", "_ja", "_en", "_cn", "_tw"} {
		if name := getString(m, prefix+"Name"+suffix); name != "" {
			return name
		}
	}
	return ""
}

func namedNames(v any, prefix string) []string {
	arr, ok := v.([]any)
	if !ok {
		if name := namedName(v, prefix); name != "" {
			return []string{name}
		}
		return nil
	}
	var out []string
	for _, item := range arr {
		if name := namedName(item, prefix); name != "" {
			out = append(out, name)
		}
	}
	return out
}

func stringList(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, item := range arr {
		if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
}

func foldNumber(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, " ", "")
	return s
}

func init() {
	provider.Register(Name, New)
}
