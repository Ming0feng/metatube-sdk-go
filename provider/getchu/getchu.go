// Package getchu implements the Getchu.com store front (www.getchu.com), the
// primary source for hentai anime (里番) and bishoujo games. Entries are
// digital store items keyed by a numeric id; search is done through
// nsearch.phtml. The adult-confirmation cookies are pre-seeded so the scraper
// is not redirected to the age gate.
package getchu

import (
	"fmt"
	"net/http"
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
	_ provider.MovieProvider = (*Getchu)(nil)
	_ provider.MovieSearcher = (*Getchu)(nil)
)

const (
	Name     = "Getchu"
	Priority = 1000
)

const (
	baseURL   = "http://www.getchu.com/"
	searchURL = "http://www.getchu.com/php/nsearch.phtml?search_keyword=%s&gc=gc"
	movieURL  = "http://www.getchu.com/soft.phtml?id=%s"
)

// idRe 匹配可选的 GETCHU- 前缀 + 纯数字 id。
var idRe = regexp.MustCompile(`^(?i)(?:GETCHU[-_])?(\d+)$`)

type Getchu struct {
	*scraper.Scraper
}

func New() *Getchu {
	return &Getchu{Scraper: scraper.NewDefaultScraper(Name, baseURL, Priority, language.Japanese,
		scraper.WithCookies(baseURL, []*http.Cookie{
			{Name: "getchu_adalt_flag", Value: "getchu.com"},
			{Name: "gc", Value: "gc"},
		}),
	)}
}

func (gcu *Getchu) NormalizeMovieID(id string) string {
	if m := idRe.FindStringSubmatch(id); m != nil {
		return m[1]
	}
	return ""
}

func (gcu *Getchu) GetMovieInfoByID(id string) (info *model.MovieInfo, err error) {
	return gcu.GetMovieInfoByURL(fmt.Sprintf(movieURL, id))
}

func (gcu *Getchu) ParseMovieIDFromURL(rawURL string) (string, error) {
	homepage, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	if id := homepage.Query().Get("id"); id != "" {
		return id, nil
	}
	return "", provider.ErrInvalidURL
}

func (gcu *Getchu) GetMovieInfoByURL(rawURL string) (info *model.MovieInfo, err error) {
	id, err := gcu.ParseMovieIDFromURL(rawURL)
	if err != nil {
		return nil, err
	}

	info = &model.MovieInfo{
		ID:            id,
		Number:        fmt.Sprintf("GETCHU-%s", id),
		Provider:      gcu.Name(),
		Homepage:      rawURL,
		Actors:        []string{},
		PreviewImages: []string{},
		Genres:        []string{},
	}

	c := gcu.ClonedCollector()

	// 标题
	c.OnXML(`//h1[@id="soft-title"]`, func(e *colly.XMLElement) {
		if info.Title == "" {
			info.Title = strings.TrimSpace(e.Text)
		}
	})

	// 封面
	c.OnXML(`//img[@class="product_img"]`, func(e *colly.XMLElement) {
		if info.CoverURL == "" {
			info.CoverURL = e.Request.AbsoluteURL(e.Attr("src"))
			info.ThumbURL = info.CoverURL
			info.BigCoverURL = info.CoverURL
		}
	})

	// 発売日
	c.OnXML(`//td[contains(text(),"発売日")]/following-sibling::td`, func(e *colly.XMLElement) {
		if time.Time(info.ReleaseDate).IsZero() {
			info.ReleaseDate = parser.ParseDate(e.Text)
		}
	})

	// ブランド
	c.OnXML(`//td[contains(text(),"ブランド")]/following-sibling::td/a`, func(e *colly.XMLElement) {
		if studio := strings.TrimSpace(e.Text); studio != "" {
			if info.Maker == "" {
				info.Maker = studio
			}
			if info.Label == "" {
				info.Label = studio
			}
		}
	})

	// ジャンル / カテゴリ
	c.OnXML(`//a[contains(@href,"genre")]`, func(e *colly.XMLElement) {
		if tag := strings.TrimSpace(e.Text); tag != "" {
			info.Genres = append(info.Genres, tag)
		}
	})

	// タイトル/カバー fallback
	c.OnXML(`//meta[@property="og:title"]`, func(e *colly.XMLElement) {
		if info.Title == "" {
			info.Title = e.Attr("content")
		}
	})
	c.OnXML(`//meta[@property="og:image"]`, func(e *colly.XMLElement) {
		if info.CoverURL == "" {
			info.CoverURL = e.Request.AbsoluteURL(e.Attr("content"))
			info.ThumbURL = info.CoverURL
		}
	})

	err = c.Visit(info.Homepage)
	return
}

func (gcu *Getchu) NormalizeMovieKeyword(keyword string) string {
	return strings.TrimSpace(keyword)
}

func (gcu *Getchu) SearchMovie(keyword string) (results []*model.MovieSearchResult, err error) {
	c := gcu.ClonedCollector()

	c.OnXML(`//a[contains(@href,"soft.phtml")]`, func(e *colly.XMLElement) {
		if len(results) > 0 {
			return // 只取首个命中。
		}
		href := e.Attr("href")
		if href == "" {
			return
		}
		homepage := e.Request.AbsoluteURL(href)
		id, _ := gcu.ParseMovieIDFromURL(homepage)
		if id == "" {
			return
		}

		results = append(results, &model.MovieSearchResult{
			ID:       id,
			Number:   fmt.Sprintf("GETCHU-%s", id),
			Title:    strings.TrimSpace(e.Text),
			Provider: gcu.Name(),
			Homepage: homepage,
		})
	})

	if err = c.Visit(fmt.Sprintf(searchURL, url.QueryEscape(keyword))); err != nil {
		return
	}
	return
}

func init() {
	provider.Register(Name, New)
}
