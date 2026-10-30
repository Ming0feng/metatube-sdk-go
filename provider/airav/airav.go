// Package airav implements a Chinese movie provider backed by airav.io. Its
// detail pages provide Chinese titles and an already-translated plot, so
// hitting this provider avoids a separate machine-translation call.
package airav

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
	_ provider.MovieProvider = (*Airav)(nil)
	_ provider.MovieSearcher = (*Airav)(nil)
)

const (
	Name     = "AiRav"
	Priority = 1000 - 20
)

const (
	// 默认 zh-TW 为繁体，这里统一走 /cn 简体版，避免插件拿到繁体标题/剧情。
	baseURL   = "https://airav.io/cn"
	searchURL = "https://airav.io/cn/search_result?kw=%s"
)

// fc2Number 匹配折叠后的 FC2 番号（fc2-<id> / fc2ppv<id>），提取商品 ID。
var fc2Number = regexp.MustCompile(`^fc2(?:ppv)?(\d+)$`)

type Airav struct {
	*scraper.Scraper
}

func New() *Airav {
	return &Airav{Scraper: scraper.NewDefaultScraper(Name, baseURL, Priority, language.Chinese)}
}

func (av *Airav) NormalizeMovieID(id string) string {
	return strings.TrimSpace(id)
}

func (av *Airav) GetMovieInfoByID(id string) (info *model.MovieInfo, err error) {
	// airav 没有固定 ID→URL 映射，详情地址由搜索定位。
	return av.getMovieInfoBySearch(id)
}

func (av *Airav) ParseMovieIDFromURL(rawURL string) (string, error) {
	homepage, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	// 详情页 URL 形如 /video?hid=<id>，ID 即 hid 参数。
	if id := homepage.Query().Get("hid"); id != "" {
		return id, nil
	}
	return "", provider.ErrInvalidURL
}

func (av *Airav) GetMovieInfoByURL(rawURL string) (info *model.MovieInfo, err error) {
	return av.getMovieInfo(rawURL)
}

func (av *Airav) getMovieInfoBySearch(id string) (info *model.MovieInfo, err error) {
	results, err := av.SearchMovie(id)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, provider.ErrInfoNotFound
	}
	return av.getMovieInfo(results[0].Homepage)
}

func (av *Airav) getMovieInfo(rawURL string) (info *model.MovieInfo, err error) {
	info = &model.MovieInfo{
		Provider:      av.Name(),
		Homepage:      rawURL,
		Actors:        []string{},
		PreviewImages: []string{},
		Genres:        []string{},
	}

	c := av.ClonedCollector()

	// 番号（信息表「番号」栏，站点为繁体「番號」）。
	c.OnXML(`//li[contains(., "番号") or contains(., "番號")]/span`, func(e *colly.XMLElement) {
		if number := strings.TrimSpace(e.Text); number != "" && info.Number == "" {
			info.Number = number
			info.ID = number
		}
	})

	// 标题（h1 以番号开头）。
	c.OnXML(`//div[contains(@class, "video-title")]//h1`, func(e *colly.XMLElement) {
		if info.Title == "" {
			info.Title = strings.TrimSpace(e.Text)
		}
	})

	// 演员
	c.OnXML(`//a[contains(@href, "actor?id=")]`, func(e *colly.XMLElement) {
		if actor := strings.TrimSpace(e.Text); actor != "" {
			info.Actors = append(info.Actors, actor)
		}
	})

	// 片商 / 发行商
	c.OnXML(`//a[contains(@href, "tag?fid=")]`, func(e *colly.XMLElement) {
		if studio := strings.TrimSpace(e.Text); studio != "" {
			if info.Maker == "" {
				info.Maker = studio
			}
			if info.Label == "" {
				info.Label = studio
			}
		}
	})

	// 发行日期
	c.OnXML(`//i[contains(@class, "fa-clock")]/parent::*`, func(e *colly.XMLElement) {
		if time.Time(info.ReleaseDate).IsZero() {
			info.ReleaseDate = parser.ParseDate(e.Text)
		}
	})

	// 标签
	c.OnXML(`//a[contains(@href, "tag?tid=")]`, func(e *colly.XMLElement) {
		if tag := strings.TrimSpace(e.Text); tag != "" {
			info.Genres = append(info.Genres, tag)
		}
	})

	// 封面
	c.OnXML(`//meta[@property="og:image"]`, func(e *colly.XMLElement) {
		if src := strings.TrimSpace(e.Attr("content")); src != "" && info.CoverURL == "" {
			info.CoverURL = e.Request.AbsoluteURL(src)
			info.ThumbURL = info.CoverURL
			info.BigCoverURL = info.CoverURL
			info.BigThumbURL = info.CoverURL
		}
	})

	// 中文剧情
	c.OnXML(`//div[contains(@class, "video-info")]/p`, func(e *colly.XMLElement) {
		if info.Summary == "" {
			info.Summary = strings.TrimSpace(e.Text)
		}
	})

	// 拆分标题与番号。
	c.OnScraped(func(_ *colly.Response) {
		if info.Number == "" || info.Title == "" {
			return
		}
		if token := leadingToken(info.Title); foldNumber(token) == foldNumber(info.Number) {
			info.Title = strings.TrimSpace(strings.TrimPrefix(info.Title, token))
		}
	})

	err = c.Visit(info.Homepage)
	return
}

func (av *Airav) NormalizeMovieKeyword(keyword string) string {
	return strings.ToUpper(strings.Join(strings.Fields(keyword), ""))
}

func (av *Airav) SearchMovie(keyword string) (results []*model.MovieSearchResult, err error) {
	// 站内 FC2 恒写 FC2-PPV-<id>，先试该写法，再试原样。
	terms := []string{keyword}
	if id := fc2ArticleID(keyword); id != "" {
		terms = []string{fmt.Sprintf("FC2-PPV-%s", id), keyword}
	}

	for _, term := range terms {
		if results, err = av.searchMovie(term); err != nil {
			return nil, err
		}
		if len(results) > 0 {
			return results, nil
		}
	}
	return nil, nil
}

func (av *Airav) searchMovie(keyword string) (results []*model.MovieSearchResult, err error) {
	c := av.ClonedCollector()

	c.OnXML(`//div[contains(@class, "oneVideo")]`, func(e *colly.XMLElement) {
		href := e.ChildAttr(`.//a`, "href")
		title := strings.TrimSpace(e.ChildText(`.//h5`))
		if href == "" || title == "" {
			return
		}
		if foldNumber(leadingToken(title)) != foldNumber(keyword) {
			return
		}

		homepage := e.Request.AbsoluteURL(href)

		results = append(results, &model.MovieSearchResult{
			ID:       keyword,
			Number:   keyword,
			Title:    title,
			Provider: av.Name(),
			Homepage: homepage,
		})
	})

	if err = c.Visit(fmt.Sprintf(searchURL, url.QueryEscape(keyword))); err != nil {
		return
	}
	return
}

func leadingToken(text string) string {
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

func fc2ArticleID(number string) string {
	m := fc2Number.FindStringSubmatch(foldNumber(number))
	if m == nil {
		return ""
	}
	return m[1]
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
