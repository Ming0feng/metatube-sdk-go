package javdb

import (
	"fmt"
	"net/url"
	"path"
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
	_ provider.MovieProvider = (*JavDB)(nil)
	_ provider.MovieSearcher = (*JavDB)(nil)
)

const (
	Name     = "JavDB"
	Priority = 1000 - 5
)

const (
	baseURL   = "https://javdb.com/"
	movieURL  = "https://javdb.com/v/%s"
	searchURL = "https://javdb.com/search?q=%s&locale=zh"
)

// trailingDigits splits a number into its non-numeric prefix and trailing digits.
var trailingDigits = regexp.MustCompile(`(?i)^(.*?)(\d+)$`)

type JavDB struct {
	*scraper.Scraper
}

func New() *JavDB {
	return &JavDB{scraper.NewDefaultScraper(Name, baseURL, Priority, language.Chinese)}
}

func (jav *JavDB) SetRequestTimeout(_ time.Duration) {
	jav.Scraper.SetRequestTimeout(15 * time.Second)
}

func (jav *JavDB) NormalizeMovieID(id string) string {
	return strings.TrimSpace(id)
}

func (jav *JavDB) GetMovieInfoByID(id string) (info *model.MovieInfo, err error) {
	return jav.GetMovieInfoByURL(fmt.Sprintf(movieURL, id))
}

func (jav *JavDB) ParseMovieIDFromURL(rawURL string) (string, error) {
	homepage, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	if ss := regexp.MustCompile(`^/v/([^/]+)/?$`).FindStringSubmatch(homepage.Path); len(ss) == 2 {
		return ss[1], nil
	}
	if id := path.Base(homepage.Path); id != "" && id != "/" && id != "." {
		return id, nil
	}
	return "", provider.ErrInvalidURL
}

func (jav *JavDB) GetMovieInfoByURL(rawURL string) (info *model.MovieInfo, err error) {
	id, err := jav.ParseMovieIDFromURL(rawURL)
	if err != nil {
		return
	}

	info = &model.MovieInfo{
		ID:            id,
		Provider:      jav.Name(),
		Homepage:      rawURL,
		Actors:        []string{},
		PreviewImages: []string{},
		Genres:        []string{},
	}

	c := jav.ClonedCollector()

	// Title (number prefix is stripped by the site itself)
	c.OnXML(`//h2[@class="title is-4"]/strong[@class="current-title"]`, func(e *colly.XMLElement) {
		info.Title = strings.TrimSpace(e.Text)
	})

	// Number
	c.OnXML(`//a[contains(@class,"copy-to-clipboard")]/@data-clipboard-text`, func(e *colly.XMLElement) {
		info.Number = strings.ToUpper(strings.TrimSpace(e.Text))
	})

	// Maker (片商)
	c.OnXML(`//strong[contains(text(),"片商")]/following-sibling::span[contains(@class,"value")]/a[1]`, func(e *colly.XMLElement) {
		if info.Maker == "" {
			info.Maker = strings.TrimSpace(e.Text)
		}
	})

	// Release Date (日期)
	c.OnXML(`//strong[contains(text(),"日期")]/following-sibling::span[contains(@class,"value")][1]`, func(e *colly.XMLElement) {
		info.ReleaseDate = parser.ParseDate(e.Text)
	})

	// Runtime (時長)
	c.OnXML(`//strong[contains(text(),"時長")]/following-sibling::span[contains(@class,"value")][1]`, func(e *colly.XMLElement) {
		info.Runtime = parser.ParseInt(regexp.MustCompile(`\d+`).FindString(e.Text))
	})

	// Director (導演)
	c.OnXML(`//strong[contains(text(),"導演")]/following-sibling::span[contains(@class,"value")]/a[1]`, func(e *colly.XMLElement) {
		if info.Director == "" {
			info.Director = strings.TrimSpace(e.Text)
		}
	})

	// Series (系列)
	c.OnXML(`//strong[contains(text(),"系列")]/following-sibling::span[contains(@class,"value")]/a[1]`, func(e *colly.XMLElement) {
		if info.Series == "" {
			info.Series = strings.TrimSpace(e.Text)
		}
	})

	// Genres (類別)
	c.OnXML(`//strong[contains(text(),"類別")]/following-sibling::span[contains(@class,"value")]/a`, func(e *colly.XMLElement) {
		if genre := strings.TrimSpace(e.Text); genre != "" {
			info.Genres = append(info.Genres, genre)
		}
	})

	// Actors (演員)
	c.OnXML(`//strong[contains(text(),"演員")]/following-sibling::span[contains(@class,"value")]/a`, func(e *colly.XMLElement) {
		if actor := strings.TrimSpace(e.Text); actor != "" {
			info.Actors = append(info.Actors, actor)
		}
	})

	// Cover
	c.OnXML(`//img[@class="video-cover"]/@src`, func(e *colly.XMLElement) {
		if src := strings.TrimSpace(e.Text); src != "" {
			info.CoverURL = e.Request.AbsoluteURL(src)
			info.ThumbURL = info.CoverURL
			info.BigCoverURL = info.CoverURL
			info.BigThumbURL = info.CoverURL
		}
	})

	// Score (評分)
	c.OnXML(`//span[@class="score-stars"]/following-sibling::text()[1]`, func(e *colly.XMLElement) {
		if ss := regexp.MustCompile(`(\d+(?:\.\d+)?)`).FindStringSubmatch(e.Text); len(ss) == 2 {
			info.Score = parser.ParseScore(ss[1])
		}
	})

	// Preview Images
	c.OnXML(`//div[contains(@class,"preview-images")]/a[contains(@class,"tile-item")]/@href`, func(e *colly.XMLElement) {
		if src := strings.TrimSpace(e.Text); src != "" {
			info.PreviewImages = append(info.PreviewImages, e.Request.AbsoluteURL(src))
		}
	})

	// Preview Video
	c.OnXML(`//video[@id="preview-video"]/source/@src`, func(e *colly.XMLElement) {
		if src := strings.TrimSpace(e.Text); src != "" {
			info.PreviewVideoURL = e.Request.AbsoluteURL(src)
		}
	})

	err = c.Visit(info.Homepage)
	return
}

func (jav *JavDB) NormalizeMovieKeyword(keyword string) string {
	keyword = strings.ToUpper(strings.Join(strings.Fields(keyword), ""))
	// JavDB pads the numeric part to 3 digits, e.g. "DSAM-13" is listed
	// as "DSAM-013"; searching the unpadded form matches the wrong entries.
	if ss := trailingDigits.FindStringSubmatch(keyword); len(ss) == 3 && len(ss[2]) < 3 {
		keyword = ss[1] + fmt.Sprintf("%03s", ss[2])
	}
	return keyword
}

func (jav *JavDB) SearchMovie(keyword string) (results []*model.MovieSearchResult, err error) {
	for _, term := range searchTerms(keyword) {
		if results, err = jav.searchMovie(term); err != nil {
			return nil, err
		}
		if len(results) > 0 {
			return results, nil
		}
	}
	return nil, nil
}

// searchTerms returns the keyword variants worth trying, e.g. both
// "DSAM-013" and "DSAM-13" in case the site lists only one of them.
func searchTerms(keyword string) []string {
	terms := []string{keyword}
	if ss := trailingDigits.FindStringSubmatch(keyword); len(ss) == 3 {
		if unpadded := strings.TrimLeft(ss[2], "0"); unpadded != ss[2] && unpadded != "" {
			terms = append(terms, ss[1]+unpadded)
		}
		if len(ss[2]) < 3 {
			terms = append(terms, ss[1]+fmt.Sprintf("%03s", ss[2]))
		}
	}
	return terms
}

func (jav *JavDB) searchMovie(keyword string) (results []*model.MovieSearchResult, err error) {
	c := jav.ClonedCollector()

	type candidate struct {
		result *model.MovieSearchResult
		number string
	}
	var candidates []candidate

	c.OnXML(`//a[@class="box"]`, func(e *colly.XMLElement) {
		homepage := e.Request.AbsoluteURL(e.Attr("href"))
		id, parseErr := jav.ParseMovieIDFromURL(homepage)
		if parseErr != nil {
			return
		}
		number := strings.TrimSpace(e.ChildText(`.//div[@class="video-title"]/strong`))
		title := strings.TrimSpace(e.Attr("title"))
		if title == "" {
			title = strings.TrimSpace(strings.TrimPrefix(e.ChildText(`.//div[@class="video-title"]`), number))
		}
		if number == "" || title == "" {
			return
		}
		candidates = append(candidates, candidate{
			number: number,
			result: &model.MovieSearchResult{
				ID:          id,
				Number:      strings.ToUpper(number),
				Title:       title,
				Provider:    jav.Name(),
				Homepage:    homepage,
				ThumbURL:    e.Request.AbsoluteURL(e.ChildAttr(`.//div[@class="cover"]/img`, "src")),
				CoverURL:    e.Request.AbsoluteURL(e.ChildAttr(`.//div[@class="cover"]/img`, "src")),
				Score:       parser.ParseScore(regexp.MustCompile(`\d+(?:\.\d+)?`).FindString(e.ChildText(`.//div[@class="score"]`))),
				ReleaseDate: parser.ParseDate(e.ChildText(`.//div[@class="meta"]`)),
			},
		})
	})

	if err = c.Visit(fmt.Sprintf(searchURL, url.QueryEscape(keyword))); err != nil {
		return
	}

	// Prefer exact number matches, then zero-padding-insensitive matches,
	// otherwise keep all candidates for the engine to rank.
	target := looseNumber(keyword)
	var exact, similar []*model.MovieSearchResult
	for _, item := range candidates {
		if strings.EqualFold(strings.TrimSpace(item.number), keyword) {
			exact = append(exact, item.result)
		} else if looseNumber(item.number) == target {
			similar = append(similar, item.result)
		}
	}
	if len(exact) > 0 {
		return exact, nil
	}
	if len(similar) > 0 {
		return similar, nil
	}
	for _, item := range candidates {
		results = append(results, item.result)
	}
	return
}

// looseNumber normalizes a number for loose comparison. JavDB omits the
// leading digit segment of some numbers (e.g. "118ABP-559" -> "ABP-559"),
// so digits and separators are ignored here.
func looseNumber(s string) string {
	s = strings.ToLower(s)
	s = regexp.MustCompile(`[^a-z\d]`).ReplaceAllString(s, "")
	s = strings.TrimLeft(s, "0123456789")
	// Treat zero-padded variants as equal: "DSAM-13" == "DSAM-013".
	if ss := trailingDigits.FindStringSubmatch(s); len(ss) == 3 && len(ss[2]) < 3 {
		s = ss[1] + fmt.Sprintf("%03s", ss[2])
	}
	return s
}

func init() {
	provider.Register(Name, New)
}
