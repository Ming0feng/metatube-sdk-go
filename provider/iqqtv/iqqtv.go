// Package iqqtv implements a Chinese movie provider backed by iqqtv
// (iqq5.xyz and its rotating mirrors). Its detail pages carry Chinese titles
// and an already-translated plot, so hitting this provider avoids a separate
// machine-translation call on the plugin side.
package iqqtv

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
	_ provider.MovieProvider = (*Iqqtv)(nil)
	_ provider.MovieSearcher = (*Iqqtv)(nil)
)

const (
	Name     = "IQQTV"
	Priority = 1000 - 20
)

const (
	baseURL   = "https://iqq5.xyz/"
	searchURL = "https://iqq5.xyz/cn/search.php?kw=%s"
	movieURL  = "https://iqq5.xyz/cn/player.php?uuid=%s"
)

// excludedKeywords 排除马赛克破坏版等无有效元数据的条目。
var excludedKeywords = []string{"克破", "无码破解", "無碼破解", "无码流出", "無碼流出"}

// langPrefix 匹配详情页 href 上的 /cn/ 或 /jp/ 语言前缀。
var langPrefix = regexp.MustCompile(`^/(cn|jp)/`)

type Iqqtv struct {
	*scraper.Scraper
}

func New() *Iqqtv {
	return &Iqqtv{Scraper: scraper.NewDefaultScraper(Name, baseURL, Priority, language.Chinese)}
}

func (iq *Iqqtv) NormalizeMovieID(id string) string {
	return strings.TrimSpace(id)
}

func (iq *Iqqtv) GetMovieInfoByID(id string) (info *model.MovieInfo, err error) {
	return iq.GetMovieInfoByURL(fmt.Sprintf(movieURL, id))
}

func (iq *Iqqtv) ParseMovieIDFromURL(rawURL string) (string, error) {
	homepage, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	// 详情页 URL 形如 /cn/player.php?uuid=<id>，ID 即 uuid 参数。
	if id := homepage.Query().Get("uuid"); id != "" {
		return id, nil
	}
	return "", provider.ErrInvalidURL
}

func (iq *Iqqtv) GetMovieInfoByURL(rawURL string) (info *model.MovieInfo, err error) {
	info = &model.MovieInfo{
		Provider:      iq.Name(),
		Homepage:      rawURL,
		Actors:        []string{},
		PreviewImages: []string{},
		Genres:        []string{},
	}

	c := iq.ClonedCollector()

	// 标题（含番号，稍后在 OnScraped 里拆分）。
	c.OnXML(`//h1[@class="h4 b"]`, func(e *colly.XMLElement) {
		if info.Title == "" {
			info.Title = strings.TrimSpace(e.Text)
		}
	})

	// 演员
	c.OnXML(`//a[contains(@href, "actor")]/span`, func(e *colly.XMLElement) {
		if actor := strings.TrimSpace(e.Text); actor != "" {
			info.Actors = append(info.Actors, actor)
		}
	})

	// 片商 / 发行商
	c.OnXML(`//a[contains(@href, "fac")]/div`, func(e *colly.XMLElement) {
		if maker := strings.TrimSpace(e.Text); maker != "" {
			if info.Maker == "" {
				info.Maker = maker
			}
			if info.Label == "" {
				info.Label = maker
			}
		}
	})

	// 发行日期
	c.OnXML(`//div[@class="date"]`, func(e *colly.XMLElement) {
		if time.Time(info.ReleaseDate).IsZero() {
			info.ReleaseDate = parser.ParseDate(e.Text)
		}
	})

	// 时长
	c.OnXML(`//meta[@itemprop="duration"]`, func(e *colly.XMLElement) {
		if info.Runtime == 0 {
			info.Runtime = parser.ParseRuntime(e.Attr("content"))
		}
	})

	// 标签
	c.OnXML(`//div[contains(@class,"tag-info")]//a[contains(@href, "tag")]`, func(e *colly.XMLElement) {
		if tag := strings.TrimSpace(e.Text); tag != "" {
			info.Genres = append(info.Genres, tag)
		}
	})

	// 系列
	c.OnXML(`//a[contains(@href, "series")]`, func(e *colly.XMLElement) {
		if info.Series == "" {
			info.Series = strings.TrimSpace(e.Text)
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
	c.OnXML(`//p[contains(., "简介") or contains(., "簡介")]`, func(e *colly.XMLElement) {
		if info.Summary == "" {
			info.Summary = cleanPlot(e.Text)
		}
	})

	// 预览图
	c.OnXML(`//div[@class="cover"]//img[@src]/@data-src`, func(e *colly.XMLElement) {
		if src := strings.TrimSpace(e.Text); src != "" {
			info.PreviewImages = append(info.PreviewImages, e.Request.AbsoluteURL(src))
		}
	})

	// 拆分标题与番号。
	c.OnScraped(func(_ *colly.Response) {
		if title, number := splitTitle(info.Title); title != "" && number != "" {
			info.Title = title
			info.Number = number
			info.ID = number
		}
	})

	err = c.Visit(info.Homepage)
	return
}

func (iq *Iqqtv) NormalizeMovieKeyword(keyword string) string {
	return strings.ToUpper(strings.Join(strings.Fields(keyword), ""))
}

func (iq *Iqqtv) SearchMovie(keyword string) (results []*model.MovieSearchResult, err error) {
	c := iq.ClonedCollector()

	c.OnXML(`//span[@class="title"]/a`, func(e *colly.XMLElement) {
		href := e.Attr("href")
		title := strings.TrimSpace(e.Attr("title"))
		if href == "" || title == "" {
			return
		}
		for _, kw := range excludedKeywords {
			if strings.Contains(title, kw) {
				return
			}
		}
		if !titleContainsNumber(title, keyword) {
			return
		}

		// 统一走 /cn 语言前缀。
		href = langPrefix.ReplaceAllString(href, "/cn/")
		homepage := e.Request.AbsoluteURL(href)
		id, _ := iq.ParseMovieIDFromURL(homepage)

		results = append(results, &model.MovieSearchResult{
			ID:       id,
			Number:   keyword,
			Title:    title,
			Provider: iq.Name(),
			Homepage: homepage,
		})
	})

	if err = c.Visit(fmt.Sprintf(searchURL, url.QueryEscape(keyword))); err != nil {
		return
	}
	return
}

// splitTitle 把 iqqtv 详情页标题拆成（纯标题, 番号）。番号是标题末尾的词，
// 标题则去掉末尾 1~2 个词（番号 + 可能的短标记）。
func splitTitle(title string) (cleanTitle, number string) {
	parts := strings.Fields(strings.TrimSpace(title))
	if len(parts) == 0 {
		return "", ""
	}
	number = parts[len(parts)-1]
	number = strings.ReplaceAll(number, "_1pondo_", "")
	number = strings.ReplaceAll(number, "1pondo_", "")
	number = strings.ReplaceAll(number, "caribbeancom-", "")
	number = strings.ReplaceAll(number, "caribbeancom", "")
	number = strings.ReplaceAll(number, "-PPV", "")
	number = strings.Trim(number, " _-")

	// 去掉番号；末尾若还有短标记（长度 < 5）也一并去掉。
	if len(parts) > 1 && len(parts[len(parts)-1]) < 5 {
		parts = parts[:len(parts)-1]
	}
	if len(parts) > 1 {
		parts = parts[:len(parts)-1]
	}
	return strings.TrimSpace(strings.Join(parts, " ")), number
}

func titleContainsNumber(title, keyword string) bool {
	target := foldNumber(keyword)
	for _, token := range strings.Fields(title) {
		if foldNumber(token) == target {
			return true
		}
	}
	return false
}

func cleanPlot(s string) string {
	if s == "" || strings.Contains(s, "克破") {
		return ""
	}
	s = strings.ReplaceAll(s, "简介：", "")
	s = strings.ReplaceAll(s, "簡介：", "")
	s = strings.ReplaceAll(s, "\n", "")
	s = strings.ReplaceAll(s, "\t", "")
	if i := strings.Index(s, "*根据分发"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
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
