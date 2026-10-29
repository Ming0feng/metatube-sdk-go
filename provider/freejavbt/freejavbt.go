// Package freejavbt implements a BT-oriented movie index backed by
// freejavbt.com. It explicitly partitions censored / uncensored / western /
// FC2 / Chinese / hentai, making it a good fallback source for several content
// types at once.
package freejavbt

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/gocolly/colly/v2"
	"golang.org/x/text/language"

	"github.com/metatube-community/metatube-sdk-go/common/parser"
	"github.com/metatube-community/metatube-sdk-go/model"
	"github.com/metatube-community/metatube-sdk-go/provider"
	"github.com/metatube-community/metatube-sdk-go/provider/internal/scraper"
)

var (
	_ provider.MovieProvider = (*Freejavbt)(nil)
	_ provider.MovieSearcher = (*Freejavbt)(nil)
)

const (
	Name     = "FREEJAVBT"
	Priority = 1000 - 40
)

const (
	baseURL   = "https://freejavbt.com/"
	searchURL = "https://freejavbt.com/search/%s"
)

// titleTailRe 匹配详情页 h1 的多语言尾注，不属于标题。
var titleTailRe = regexp.MustCompile(`(?i)\s*(?:免費AV在線看|免费AV在线看|Watch for free|Смотреть онлайн)\s*$`)

var (
	dateRe        = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	leadingDigits = regexp.MustCompile(`^\d+`)
	fc2NumberRe   = regexp.MustCompile(`^FC2(?:PPV)?(\d+)$`)
)

type Freejavbt struct {
	*scraper.Scraper
}

func New() *Freejavbt {
	return &Freejavbt{Scraper: scraper.NewDefaultScraper(Name, baseURL, Priority, language.Japanese)}
}

func (fj *Freejavbt) NormalizeMovieID(id string) string {
	return strings.TrimSpace(id)
}

func (fj *Freejavbt) GetMovieInfoByID(id string) (info *model.MovieInfo, err error) {
	return fj.GetMovieInfoByURL(fmt.Sprintf(searchURL, id))
}

func (fj *Freejavbt) ParseMovieIDFromURL(rawURL string) (string, error) {
	homepage, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	return numberFromPath(homepage.Path), nil
}

func (fj *Freejavbt) GetMovieInfoByURL(rawURL string) (info *model.MovieInfo, err error) {
	number := numberFromURL(rawURL)

	info = &model.MovieInfo{
		ID:            number,
		Number:        number,
		Provider:      fj.Name(),
		Homepage:      rawURL,
		Actors:        []string{},
		PreviewImages: []string{},
		Genres:        []string{},
	}

	c := fj.ClonedCollector()

	// 标题（h1 = 番号 + 标题 + 尾注）。
	c.OnXML(`//h1`, func(e *colly.XMLElement) {
		if info.Title == "" {
			info.Title = cleanTitle(e.Text, number)
		}
	})

	// 女优
	c.OnXML(`//a[contains(@class,"actress")]`, func(e *colly.XMLElement) {
		if actor := strings.TrimSpace(e.Text); actor != "" {
			info.Actors = append(info.Actors, actor)
		}
	})

	// 导演（取第一个）。
	c.OnXML(`//div[contains(@class,"director")]//a`, func(e *colly.XMLElement) {
		if info.Director == "" {
			info.Director = strings.TrimSpace(e.Text)
		}
	})

	// 系列
	c.OnXML(`//div[contains(@class,"series")]//a`, func(e *colly.XMLElement) {
		if info.Series == "" {
			info.Series = strings.TrimSpace(e.Text)
		}
	})

	// 无链接信息行：日期形 → 发行日，前导整数 → 片长。
	c.OnXML(`//div[contains(@class,"single-video-meta")][not(.//a)]`, func(e *colly.XMLElement) {
		value := rowValue(e.Text)
		switch {
		case dateRe.MatchString(value):
			if time.Time(info.ReleaseDate).IsZero() {
				info.ReleaseDate = parser.ParseDate(value)
			}
		case leadingDigits.MatchString(value):
			if info.Runtime == 0 {
				info.Runtime = parser.ParseInt(leadingDigits.FindString(value))
			}
		}
	})

	// 标签
	c.OnXML(`//div[contains(@class,"single-video-meta")]//a[contains(@class,"btn-danger")]`, func(e *colly.XMLElement) {
		if tag := strings.TrimSpace(e.Text); tag != "" {
			info.Genres = append(info.Genres, tag)
		}
	})

	// 封面
	c.OnXML(`//meta[@property="og:image"]`, func(e *colly.XMLElement) {
		if src := strings.TrimSpace(e.Attr("content")); src != "" && info.CoverURL == "" {
			info.CoverURL = e.Request.AbsoluteURL(src)
			info.ThumbURL = info.CoverURL
		}
	})

	// 预览图
	c.OnXML(`//div[contains(@class,"preview")]//a[contains(@class,"tile-item")]`, func(e *colly.XMLElement) {
		if src := strings.TrimSpace(e.Attr("href")); src != "" {
			info.PreviewImages = append(info.PreviewImages, e.Request.AbsoluteURL(src))
		}
	})

	err = c.Visit(info.Homepage)
	return
}

func (fj *Freejavbt) NormalizeMovieKeyword(keyword string) string {
	return strings.ToUpper(strings.Join(strings.Fields(keyword), ""))
}

func (fj *Freejavbt) SearchMovie(keyword string) (results []*model.MovieSearchResult, err error) {
	// 站内条目末段只写不带 PPV 的 FC2 写法，逐形态尝试。
	for _, term := range searchVariants(keyword) {
		if results, err = fj.searchMovie(term); err != nil {
			return nil, err
		}
		if len(results) > 0 {
			return results, nil
		}
	}
	return nil, nil
}

func (fj *Freejavbt) searchMovie(keyword string) (results []*model.MovieSearchResult, err error) {
	c := fj.ClonedCollector()

	c.OnXML(`//div[contains(@class,"category-page") and contains(@class,"video-list-item")]`, func(e *colly.XMLElement) {
		href := e.ChildAttr(`.//a`, "href")
		if href == "" {
			return
		}
		homepage := e.Request.AbsoluteURL(href)
		number := numberFromURL(homepage)
		if foldNumber(number) != foldNumber(keyword) {
			return
		}

		results = append(results, &model.MovieSearchResult{
			ID:       number,
			Number:   keyword,
			Title:    strings.TrimSpace(e.ChildText(`.//a`)),
			Provider: fj.Name(),
			Homepage: homepage,
		})
	})

	if err = c.Visit(fmt.Sprintf(searchURL, url.PathEscape(keyword))); err != nil {
		return
	}
	return
}

// searchVariants 生成搜索词变体（FC2-PPV → FC2）。
func searchVariants(keyword string) []string {
	variants := []string{keyword}
	if m := fc2NumberRe.FindStringSubmatch(strings.ToUpper(keyword)); m != nil {
		variants = append([]string{fmt.Sprintf("FC2-%s", m[1])}, variants...)
	}
	return variants
}

func cleanTitle(raw, number string) string {
	text := titleTailRe.ReplaceAllString(raw, "")
	text = strings.Join(strings.Fields(text), " ")
	if number != "" && strings.HasPrefix(strings.ToLower(text), strings.ToLower(number)) {
		text = strings.TrimSpace(text[len(number):])
	}
	return strings.TrimSpace(text)
}

func rowValue(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if i := strings.Index(text, ":"); i >= 0 {
		return strings.TrimSpace(text[i+1:])
	}
	return text
}

func numberFromURL(rawURL string) string {
	if u, err := url.Parse(rawURL); err == nil {
		return numberFromPath(u.Path)
	}
	return ""
}

func numberFromPath(path string) string {
	path = strings.TrimRight(path, "/")
	if i := strings.LastIndex(path, "/"); i >= 0 {
		path = path[i+1:]
	}
	if s, err := url.PathUnescape(path); err == nil {
		return s
	}
	return path
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
