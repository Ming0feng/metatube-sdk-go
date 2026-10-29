// Package minnano implements an actress-profile source backed by
// minnano-av.com. It provides name / aliases / birthday / birthplace / figure
// (bust / waist / hip / cup) / portrait.
package minnano

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
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
	_ provider.ActorProvider = (*Minnano)(nil)
	_ provider.ActorSearcher = (*Minnano)(nil)
)

const (
	Name     = "MINNANO"
	Priority = 1000 - 30
)

const (
	baseURL   = "https://www.minnano-av.com/"
	searchURL = "https://www.minnano-av.com/search_result.php?search_scope=actress&search_word=%s&search=Go"
)

var (
	nameLineRe    = regexp.MustCompile(`^(?P<name>.+?)\s*[（(]\s*(?P<kana>[^/（）()]+?)\s*/\s*(?P<roma>[^）)]+?)\s*[）)]\s*$`)
	figureRe      = regexp.MustCompile(`(?i)T\s*(\d+)\s*/\s*B\s*(\d+)\s*(?:\(\s*([A-Z]+)\s*\))?\s*/\s*W\s*(\d+)\s*/\s*H\s*(\d+)`)
	cupRe         = regexp.MustCompile(`(?i)([A-Z]+)カップ`)
	actressHrefRe = regexp.MustCompile(`(?i)actress\d+\.html`)
	actorIDRe     = regexp.MustCompile(`(?i)actress(\d+)\.html`)
	annotationRe  = regexp.MustCompile(`\s*[（(].*?[）)]\s*$`)
)

type Minnano struct {
	*scraper.Scraper
}

func New() *Minnano {
	return &Minnano{Scraper: scraper.NewDefaultScraper(Name, baseURL, Priority, language.Japanese,
		scraper.WithCookies(baseURL, []*http.Cookie{
			{Name: "age_verified", Value: "1"},
		}),
	)}
}

func (mn *Minnano) NormalizeActorID(id string) string {
	if m := actorIDRe.FindStringSubmatch(id); m != nil {
		return m[1]
	}
	return strings.TrimSpace(id)
}

func (mn *Minnano) GetActorInfoByID(id string) (*model.ActorInfo, error) {
	return mn.GetActorInfoByURL(fmt.Sprintf("https://www.minnano-av.com/actress%s.html", id))
}

func (mn *Minnano) ParseActorIDFromURL(rawURL string) (string, error) {
	homepage, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	if m := actorIDRe.FindStringSubmatch(homepage.Path); m != nil {
		return m[1], nil
	}
	return "", provider.ErrInvalidURL
}

func (mn *Minnano) GetActorInfoByURL(rawURL string) (info *model.ActorInfo, err error) {
	id, err := mn.ParseActorIDFromURL(rawURL)
	if err != nil {
		return nil, err
	}

	info = &model.ActorInfo{
		ID:       id,
		Provider: mn.Name(),
		Homepage: rawURL,
		Aliases:  []string{},
		Images:   []string{},
	}

	c := mn.ClonedCollector()

	// 名字行（名字 + 读音别名）。
	c.OnXML(`//div[contains(@class,"act-profile")]//h2`, func(e *colly.XMLElement) {
		if info.Name != "" {
			return
		}
		if name, aliases := parseNameLine(e.Text); name != "" {
			info.Name = name
			info.Aliases = append(info.Aliases, aliases...)
		}
	})

	// 別名行。
	c.OnXML(`//div[contains(@class,"act-profile")]//tr[td/span[normalize-space()="別名"]]`, func(e *colly.XMLElement) {
		raw := strings.TrimSpace(e.ChildText(`.//td/p`))
		if name, aliases := parseNameLine(raw); name != "" {
			info.Aliases = append(info.Aliases, name)
			info.Aliases = append(info.Aliases, aliases...)
		}
	})

	// 信息行（生年月日 / 出身地 / サイズ）。
	c.OnXML(`//div[contains(@class,"act-profile")]//tr`, func(e *colly.XMLElement) {
		key := strings.TrimSpace(e.ChildText(`./td/span`))
		value := strings.TrimSpace(e.ChildText(`./td/p`))
		if value == "" {
			value = strings.TrimSpace(e.ChildText(`./td`))
		}
		switch key {
		case "生年月日":
			if time.Time(info.Birthday).IsZero() {
				info.Birthday = parser.ParseDate(value)
			}
		case "サイズ":
			height, bust, waist, hip, cup := parseFigure(value)
			info.Height = height
			info.CupSize = cup
			if bust != 0 && waist != 0 && hip != 0 {
				info.Measurements = fmt.Sprintf("B:%d / W:%d / H:%d", bust, waist, hip)
			}
		}
	})

	// 头像。
	c.OnXML(`//meta[@property="og:image"]`, func(e *colly.XMLElement) {
		if src := strings.TrimSpace(e.Attr("content")); src != "" {
			info.Images = append(info.Images, e.Request.AbsoluteURL(src))
		}
	})

	info.Aliases = dedupeNames(info.Aliases, info.Name)

	err = c.Visit(info.Homepage)
	return
}

func (mn *Minnano) SearchActor(name string) (results []*model.ActorSearchResult, err error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil
	}

	c := mn.ClonedCollector()

	var detailURL string
	// 精确命中常 302 到详情页。
	c.OnXML(`//div[contains(@class,"act-profile")]`, func(e *colly.XMLElement) {
		if detailURL == "" {
			detailURL = e.Request.AbsoluteURL(e.ChildAttr(`//link[@rel="canonical"]`, "href"))
		}
	})

	// 搜索结果表。
	c.OnXML(`//table[contains(@class,"tbllist") and contains(@class,"actress")]//tr[td]`, func(e *colly.XMLElement) {
		title := strings.TrimSpace(e.ChildText(`.//h2[contains(@class,"ttl")]/a`))
		href := e.ChildAttr(`.//h2[contains(@class,"ttl")]/a`, "href")
		if title == "" || !actressHrefRe.MatchString(href) {
			return
		}
		if !sameName(stripAnnotations(title), name) {
			return
		}
		homepage := e.Request.AbsoluteURL(strings.Split(href, "?")[0])
		results = append(results, &model.ActorSearchResult{
			ID:       mn.NormalizeActorID(actorIDFromHref(href)),
			Name:     title,
			Provider: mn.Name(),
			Homepage: homepage,
		})
	})

	if err = c.Visit(fmt.Sprintf(searchURL, url.QueryEscape(name))); err != nil {
		return
	}

	if detailURL != "" {
		id, _ := mn.ParseActorIDFromURL(detailURL)
		return []*model.ActorSearchResult{{
			ID:       id,
			Name:     name,
			Provider: mn.Name(),
			Homepage: detailURL,
		}}, nil
	}
	return
}

func parseNameLine(raw string) (name string, aliases []string) {
	text := strings.TrimSpace(raw)
	m := nameLineRe.FindStringSubmatch(text)
	if m == nil {
		return text, nil
	}
	name = stripAnnotations(strings.TrimSpace(m[1]))
	if kana := strings.TrimSpace(m[2]); kana != "" {
		aliases = append(aliases, kana)
	}
	if roma := strings.TrimSpace(m[3]); roma != "" {
		aliases = append(aliases, roma)
	}
	return
}

func parseFigure(raw string) (height, bust, waist, hip int, cup string) {
	text := strings.TrimSpace(raw)
	if m := figureRe.FindStringSubmatch(text); m != nil {
		height = atoi(m[1])
		bust = atoi(m[2])
		if m[3] != "" {
			cup = strings.ToUpper(m[3])
		}
		waist = atoi(m[4])
		hip = atoi(m[5])
	}
	if cup == "" {
		if m := cupRe.FindStringSubmatch(text); m != nil {
			cup = strings.ToUpper(m[1])
		}
	}
	return
}

func stripAnnotations(name string) string {
	for {
		stripped := annotationRe.ReplaceAllString(name, "")
		if strings.TrimSpace(stripped) == strings.TrimSpace(name) {
			return strings.TrimSpace(name)
		}
		name = stripped
	}
}

func actorIDFromHref(href string) string {
	if m := actorIDRe.FindStringSubmatch(href); m != nil {
		return m[1]
	}
	return ""
}

func dedupeNames(values []string, exclude string) []string {
	var out []string
	for _, v := range values {
		if v == "" || sameName(v, exclude) {
			continue
		}
		dup := false
		for _, existing := range out {
			if sameName(v, existing) {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, v)
		}
	}
	return out
}

func sameName(a, b string) bool {
	return normalizeName(a) == normalizeName(b)
}

func normalizeName(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), ""))
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

func init() {
	provider.Register(Name, New)
}
