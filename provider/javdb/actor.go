package javdb

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/gocolly/colly/v2"
	"golang.org/x/text/language"

	"github.com/metatube-community/metatube-sdk-go/model"
	"github.com/metatube-community/metatube-sdk-go/provider"
	"github.com/metatube-community/metatube-sdk-go/provider/internal/scraper"
)

var (
	_ provider.ActorProvider = (*JavDBActor)(nil)
	_ provider.ActorSearcher = (*JavDBActor)(nil)
)

const (
	ActorProviderName = "JavDBActor"
	actorPriority     = 1000 - 5

	actorSearchURL = "https://javdb.com/search?q=%s&f=actor&locale=zh"
	actorURL       = "https://javdb.com/actors/%s"
)

var (
	actorPathRe   = regexp.MustCompile(`^/actors/([A-Za-z0-9]+)/?$`)
	actorSlugs    = map[string]struct{}{"censored": {}, "uncensored": {}, "western": {}}
	bgURLRe       = regexp.MustCompile(`url\(\s*['"]?(https?://[^)'"]+)['"]?\s*\)`)
	placeholderRe = regexp.MustCompile(`actor_unknow`)
	filmCountRe   = regexp.MustCompile(`\d+\s*(?:部影片|movie\(s\))`)
)

type JavDBActor struct {
	*scraper.Scraper
}

func NewActor() *JavDBActor {
	return &JavDBActor{Scraper: scraper.NewDefaultScraper(ActorProviderName, baseURL, actorPriority, language.Chinese)}
}

func (ja *JavDBActor) NormalizeActorID(id string) string {
	return strings.TrimSpace(id)
}

func (ja *JavDBActor) GetActorInfoByID(id string) (*model.ActorInfo, error) {
	return ja.GetActorInfoByURL(fmt.Sprintf(actorURL, id))
}

func (ja *JavDBActor) ParseActorIDFromURL(rawURL string) (string, error) {
	homepage, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	if m := actorPathRe.FindStringSubmatch(homepage.Path); m != nil {
		if _, ok := actorSlugs[strings.ToLower(m[1])]; !ok {
			return m[1], nil
		}
	}
	return "", provider.ErrInvalidURL
}

func (ja *JavDBActor) GetActorInfoByURL(rawURL string) (info *model.ActorInfo, err error) {
	id, err := ja.ParseActorIDFromURL(rawURL)
	if err != nil {
		return nil, err
	}

	info = &model.ActorInfo{
		ID:       id,
		Provider: ja.Name(),
		Homepage: rawURL,
		Aliases:  []string{},
		Images:   []string{},
	}

	c := ja.ClonedCollector()

	// 名字（逗号分隔的多个名義）。
	c.OnXML(`//span[contains(@class,"actor-section-name")]`, func(e *colly.XMLElement) {
		names := splitNames(e.Text)
		if len(names) == 0 || info.Name != "" {
			return
		}
		info.Name = names[0]
		info.Aliases = append(info.Aliases, names[1:]...)
	})

	// 别名 / 性别 / 影片数。
	c.OnXML(`//span[contains(@class,"section-meta")]`, func(e *colly.XMLElement) {
		for _, token := range splitNames(e.Text) {
			if filmCountRe.MatchString(token) || isGenderToken(token) {
				continue
			}
			if info.Name != "" && !sameName(token, info.Name) {
				info.Aliases = append(info.Aliases, token)
			}
		}
	})

	// 头像（背景图）。
	c.OnXML(`//div[contains(@class,"actor-avatar")]//span[contains(@class,"avatar")]`, func(e *colly.XMLElement) {
		if src := bgURLRe.FindStringSubmatch(e.Attr("style")); len(src) == 2 && !placeholderRe.MatchString(src[1]) {
			info.Images = append(info.Images, e.Request.AbsoluteURL(src[1]))
		}
	})

	// 去重别名。
	info.Aliases = dedupeNames(info.Aliases, info.Name)

	err = c.Visit(info.Homepage)
	return
}

func (ja *JavDBActor) SearchActor(name string) (results []*model.ActorSearchResult, err error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil
	}

	c := ja.ClonedCollector()

	c.OnXML(`//div[contains(@class,"actor-box")]//a[@href]`, func(e *colly.XMLElement) {
		href := e.Attr("href")
		actorID := actorIDFromHref(href)
		if actorID == "" {
			return
		}

		canonical := e.ChildText(`.//strong`)
		aliases := splitNames(e.Attr("title"))
		if !anySameName(append([]string{canonical}, aliases...), name) {
			return
		}

		homepage := e.Request.AbsoluteURL(href)
		results = append(results, &model.ActorSearchResult{
			ID:       actorID,
			Name:     strings.TrimSpace(canonical),
			Provider: ja.Name(),
			Homepage: homepage,
			Aliases:  aliases,
		})
	})

	if err = c.Visit(fmt.Sprintf(actorSearchURL, url.QueryEscape(name))); err != nil {
		return
	}
	return
}

func actorIDFromHref(href string) string {
	path := href
	if u, err := url.Parse(href); err == nil {
		path = u.Path
	}
	if m := actorPathRe.FindStringSubmatch(path); m != nil {
		if _, ok := actorSlugs[strings.ToLower(m[1])]; !ok {
			return m[1]
		}
	}
	return ""
}

func splitNames(raw string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '，'
	}) {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
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

func anySameName(values []string, target string) bool {
	for _, v := range values {
		if sameName(v, target) {
			return true
		}
	}
	return false
}

func sameName(a, b string) bool {
	return normalizeName(a) == normalizeName(b)
}

func normalizeName(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), ""))
}

func isGenderToken(s string) bool {
	switch s {
	case "男優", "男优", "Male", "女優", "女优", "Female":
		return true
	}
	return false
}

func init() {
	provider.Register(ActorProviderName, NewActor)
}
