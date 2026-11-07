// Package official aggregates the Will / Outvision group's official
// manufacturer sites (S1, MOODYZ, Madonna, ...). They share one CMS, so a
// single scraper covers all of them. The series prefix of a number is used to
// route to the owning manufacturer's domain; when no prefix matches, the
// provider skips the request entirely.
package official

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
	_ provider.MovieProvider = (*Official)(nil)
	_ provider.MovieSearcher = (*Official)(nil)
)

const (
	Name     = "OFFICIAL"
	Priority = 1000 - 10
)

// baseURL 仅为满足 Provider 接口（连通性探测等），官方集群无总入口。
const baseURL = "https://s1s1s1.com/"

var lettersRe = regexp.MustCompile(`^[a-zA-Z]+`)

// seriesToDomain 番号系列前缀 → 官网域名。
var seriesToDomain = map[string]string{
	// S1
	"sivr": "s1s1s1.com", "ssis": "s1s1s1.com", "ssni": "s1s1s1.com",
	"snis": "s1s1s1.com", "soe": "s1s1s1.com", "oned": "s1s1s1.com",
	"one": "s1s1s1.com", "onsd": "s1s1s1.com", "ofje": "s1s1s1.com",
	"sps": "s1s1s1.com", "tksoe": "s1s1s1.com",
	// MOODYZ
	"mdvr": "moodyz.com", "midv": "moodyz.com", "mide": "moodyz.com",
	"midd": "moodyz.com", "mibd": "moodyz.com", "mimk": "moodyz.com",
	"miid": "moodyz.com", "migd": "moodyz.com", "mifd": "moodyz.com",
	"miae": "moodyz.com", "miad": "moodyz.com", "miaa": "moodyz.com",
	"mdl": "moodyz.com", "mdj": "moodyz.com", "mdi": "moodyz.com",
	"mdg": "moodyz.com", "mdf": "moodyz.com", "mde": "moodyz.com",
	"mdld": "moodyz.com", "mded": "moodyz.com", "mizd": "moodyz.com",
	"mird": "moodyz.com", "mdjd": "moodyz.com", "rmid": "moodyz.com",
	"mdid": "moodyz.com", "mdmd": "moodyz.com", "mimu": "moodyz.com",
	"mdpd": "moodyz.com", "mivd": "moodyz.com", "mdud": "moodyz.com",
	"mdgd": "moodyz.com", "mdvd": "moodyz.com", "mias": "moodyz.com",
	"miqd": "moodyz.com", "mint": "moodyz.com", "rmpd": "moodyz.com",
	"mdrd": "moodyz.com", "tkmide": "moodyz.com", "tkmidd": "moodyz.com",
	"kmide": "moodyz.com", "tkmigd": "moodyz.com", "mdfd": "moodyz.com",
	"rmwd": "moodyz.com", "miab": "moodyz.com",
	// Madonna
	"juvr": "www.madonna-av.com", "jusd": "www.madonna-av.com", "juq": "www.madonna-av.com",
	"juy": "www.madonna-av.com", "jux": "www.madonna-av.com", "jul": "www.madonna-av.com",
	"juk": "www.madonna-av.com", "juc": "www.madonna-av.com", "jukd": "www.madonna-av.com",
	"oba": "www.madonna-av.com", "roeb": "www.madonna-av.com", "roe": "www.madonna-av.com",
	"ure": "www.madonna-av.com", "mdon": "www.madonna-av.com", "obe": "www.madonna-av.com",
	"jums": "www.madonna-av.com",
	// Wanz Factory
	"wavr": "www.wanz-factory.com", "waaa": "www.wanz-factory.com",
	"bmw": "www.wanz-factory.com", "wanz": "www.wanz-factory.com",
	// Idea Pocket
	"ipvr": "ideapocket.com", "ipx": "ideapocket.com", "ipz": "ideapocket.com",
	"iptd": "ideapocket.com", "ipsd": "ideapocket.com", "idbd": "ideapocket.com",
	"supd": "ideapocket.com", "ipit": "ideapocket.com", "and": "ideapocket.com",
	"hpd": "ideapocket.com", "tkipz": "ideapocket.com", "ipzz": "ideapocket.com",
	"cosd": "ideapocket.com", "anpd": "ideapocket.com", "dan": "ideapocket.com",
	"alad": "ideapocket.com", "kipx": "ideapocket.com",
	// kira☆kira
	"kivr": "kirakira-av.com", "blk": "kirakira-av.com", "kibd": "kirakira-av.com",
	"kifd": "kirakira-av.com", "kird": "kirakira-av.com", "kisd": "kirakira-av.com",
	"set": "kirakira-av.com",
	// E-BODY
	"ebvr": "www.av-e-body.com", "ebod": "www.av-e-body.com",
	"mkck": "www.av-e-body.com", "eyan": "www.av-e-body.com",
	// BI (痴女ヘブン)
	"cjvr": "bi-av.com", "cjod": "bi-av.com", "bbi": "bi-av.com",
	"bib": "bi-av.com", "cjob": "bi-av.com", "beb": "bi-av.com",
	"bid": "bi-av.com", "bist": "bi-av.com", "bwb": "bi-av.com",
	// PREMIUM
	"prvr": "premium-beauty.com", "pgd": "premium-beauty.com", "pred": "premium-beauty.com",
	"pbd": "premium-beauty.com", "pjd": "premium-beauty.com", "prtd": "premium-beauty.com",
	"pxd": "premium-beauty.com", "pid": "premium-beauty.com", "ptv": "premium-beauty.com",
	// Miman (未満)
	"mmvr": "miman.jp", "mmnd": "miman.jp", "mmxd": "miman.jp", "aom": "miman.jp",
	// 溜池ゴロー
	"mevr": "tameikegoro.jp", "meyd": "tameikegoro.jp", "mbyd": "tameikegoro.jp",
	"mdyd": "tameikegoro.jp", "mnyd": "tameikegoro.jp",
	// Fitch
	"fcvr": "fitch-av.com", "jufe": "fitch-av.com", "jufd": "fitch-av.com",
	"jfb": "fitch-av.com", "juny": "fitch-av.com", "nyb": "fitch-av.com",
	"finh": "fitch-av.com", "gcf": "fitch-av.com", "nima": "fitch-av.com",
	// kawaii*
	"kavr": "kawaiikawaii.jp", "cawd": "kawaiikawaii.jp", "kwbd": "kawaiikawaii.jp",
	"kawd": "kawaiikawaii.jp", "kwsr": "kawaiikawaii.jp", "kwsd": "kawaiikawaii.jp",
	"kane": "kawaiikawaii.jp",
	// BeFree
	"bf": "befreebe.com",
	// 無垢
	"mucd": "muku.tv", "mudr": "muku.tv", "mukd": "muku.tv", "smcd": "muku.tv", "mukc": "muku.tv",
	// ATTACKERS
	"atvr": "attackers.net", "rbk": "attackers.net", "rbd": "attackers.net",
	"same": "attackers.net", "shkd": "attackers.net", "atid": "attackers.net",
	"adn": "attackers.net", "atkd": "attackers.net", "jbd": "attackers.net", //nolint:misspell // adn 是番号前缀，非 and 拼写
	"sspd": "attackers.net", "atad": "attackers.net", "azsd": "attackers.net",
	// えむっ娘ラボ
	"mvr": "mko-labo.net", "mism": "mko-labo.net", "emlb": "mko-labo.net",
	// Das!
	"dsvr": "dasdas.jp", "dass": "dasdas.jp", "dazd": "dasdas.jp",
	"dasd": "dasdas.jp", "pla": "dasdas.jp",
	// MVG
	"mvbd": "mvg.jp", "mvsd": "mvg.jp",
	// OPERA
	"opvr": "av-opera.jp", "opbd": "av-opera.jp", "opud": "av-opera.jp",
	// OPPAI
	"ppvr": "oppai-av.com", "pppe": "oppai-av.com", "ppbd": "oppai-av.com",
	"pppd": "oppai-av.com", "ppsd": "oppai-av.com", "ppfd": "oppai-av.com",
	// V
	"vvvd": "v-av.com", "vicd": "v-av.com", "vizd": "v-av.com", "vspd": "v-av.com",
	// 変態紳士倶楽部
	"clvr": "to-satsu.com", "stol": "to-satsu.com", "club": "to-satsu.com",
	// ビビアン
	"bbvr": "bibian-av.com", "bban": "bibian-av.com",
	// 本中
	"hnvr": "honnaka.jp", "hmn": "honnaka.jp", "hndb": "honnaka.jp",
	"hnd": "honnaka.jp", "krnd": "honnaka.jp", "hnky": "honnaka.jp",
	"hnjc": "honnaka.jp", "hnse": "honnaka.jp",
	// Rookie
	"rvr": "rookie-av.jp", "rbb": "rookie-av.jp", "rki": "rookie-av.jp",
	// ナンパJAPAN
	"njvr": "nanpa-japan.jp", "nnpj": "nanpa-japan.jp", "npjb": "nanpa-japan.jp",
	// はじめ企画
	"hjbb": "hajimekikaku.com", "hjmo": "hajimekikaku.com", "avgl": "hajimekikaku.com",
	// Hunter
	"huntb": "hhh-av.com", "hunta": "hhh-av.com", "hunt": "hhh-av.com",
	"hunbl": "hhh-av.com", "royd": "hhh-av.com", "tysf": "hhh-av.com",
}

type Official struct {
	*scraper.Scraper
}

func New() *Official {
	return &Official{Scraper: scraper.NewDefaultScraper(Name, baseURL, Priority, language.Japanese)}
}

func (off *Official) NormalizeMovieID(id string) string {
	return strings.ToUpper(strings.Join(strings.Fields(id), ""))
}

func (off *Official) GetMovieInfoByID(id string) (info *model.MovieInfo, err error) {
	domain := resolveDomain(id)
	if domain == "" {
		return nil, provider.ErrInfoNotFound
	}
	return off.GetMovieInfoByURL(detailURL(domain, id))
}

func (off *Official) ParseMovieIDFromURL(rawURL string) (string, error) {
	homepage, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	segments := strings.Split(strings.Trim(homepage.Path, "/"), "/")
	if len(segments) >= 2 && segments[len(segments)-2] == "detail" {
		return segments[len(segments)-1], nil
	}
	return "", provider.ErrInvalidURL
}

func (off *Official) GetMovieInfoByURL(rawURL string) (info *model.MovieInfo, err error) {
	id, err := off.ParseMovieIDFromURL(rawURL)
	if err != nil {
		return nil, err
	}

	info = &model.MovieInfo{
		ID:            id,
		Number:        strings.ToUpper(id),
		Provider:      off.Name(),
		Homepage:      rawURL,
		Actors:        []string{},
		PreviewImages: []string{},
		Genres:        []string{},
	}

	c := off.ClonedCollector()

	// 品番
	c.OnXML(`//div[@class="th"][contains(text(),"品番")]/following-sibling::div[@class="td"]//p`, func(e *colly.XMLElement) {
		if number := extractNumber(e.Text); number != "" && info.Number == "" {
			info.Number = number
			info.ID = number
		}
	})

	// 标题
	c.OnXML(`//title`, func(e *colly.XMLElement) {
		info.Title = cleanTitle(e.Text)
		if studio := extractStudio(e.Text); studio != "" && info.Maker == "" {
			info.Maker = studio
		}
	})

	// 女优（仅信息表「女優」栏，避免推荐区误抓）。
	c.OnXML(`//div[@class="th"][contains(text(),"女優")]/following-sibling::div[@class="td"]//a[contains(@href,"/actress/detail/")]`, func(e *colly.XMLElement) {
		if actor := strings.TrimSpace(e.Text); actor != "" {
			info.Actors = append(info.Actors, actor)
		}
	})

	// 発売日
	c.OnXML(`//div[@class="th"][contains(text(),"発売日")]/following-sibling::div[@class="td"]//a`, func(e *colly.XMLElement) {
		if time.Time(info.ReleaseDate).IsZero() {
			info.ReleaseDate = parser.ParseDate(e.Text)
		}
	})

	// 収録時間
	c.OnXML(`//div[@class="th"][contains(text(),"収録時間")]/following-sibling::div[@class="td"]//p`, func(e *colly.XMLElement) {
		if info.Runtime == 0 {
			info.Runtime = parseMinutes(e.Text)
		}
	})

	// ジャンル
	c.OnXML(`//div[@class="th"][contains(text(),"ジャンル")]/following-sibling::div[@class="td"]//a[contains(@href,"/works/list/genre/")]`, func(e *colly.XMLElement) {
		if tag := strings.TrimSpace(e.Text); tag != "" {
			info.Genres = append(info.Genres, tag)
		}
	})

	// 封面（作品轮播首图）
	c.OnXML(`//div[contains(@class,"p-slider")]//img[contains(@class,"swiper-lazy")]`, func(e *colly.XMLElement) {
		if src := strings.TrimSpace(e.Attr("data-src")); src != "" && info.CoverURL == "" {
			info.CoverURL = e.Request.AbsoluteURL(src)
			info.ThumbURL = info.CoverURL
			info.BigCoverURL = info.CoverURL
		}
	})

	// シリーズ
	c.OnXML(`//div[@class="th"][contains(text(),"シリーズ")]/following-sibling::div[@class="item"]//a`, func(e *colly.XMLElement) {
		if info.Series == "" {
			info.Series = strings.TrimSpace(e.Text)
		}
	})

	err = c.Visit(info.Homepage)
	return
}

func (off *Official) NormalizeMovieKeyword(keyword string) string {
	return strings.ToUpper(strings.Join(strings.Fields(keyword), ""))
}

func (off *Official) SearchMovie(keyword string) (results []*model.MovieSearchResult, err error) {
	domain := resolveDomain(keyword)
	if domain == "" {
		return nil, nil
	}
	homepage := detailURL(domain, keyword)
	results = append(results, &model.MovieSearchResult{
		ID:       keyword,
		Number:   keyword,
		Title:    keyword,
		Provider: off.Name(),
		Homepage: homepage,
	})
	return results, nil
}

// resolveDomain 根据番号系列前缀路由到官网域名；无匹配返回空串。
func resolveDomain(number string) string {
	prefix := extractSeriesPrefix(number)
	return seriesToDomain[prefix]
}

func detailURL(domain, number string) string {
	return fmt.Sprintf("https://%s/works/detail/%s", domain, normalizeNumber(number))
}

// normalizeNumber 官网详情 URL 用去掉横线的小写番号。
func normalizeNumber(number string) string {
	return strings.ToLower(strings.ReplaceAll(strings.ToUpper(number), "-", ""))
}

// extractSeriesPrefix 提取番号开头的字母序列（去分隔符与前导数字）。
func extractSeriesPrefix(number string) string {
	cleaned := strings.ReplaceAll(number, "-", "")
	cleaned = strings.ReplaceAll(cleaned, "_", "")
	cleaned = strings.TrimLeft(cleaned, "0123456789")
	return strings.ToLower(lettersRe.FindString(cleaned))
}

func extractNumber(raw string) string {
	raw = strings.TrimSpace(raw)
	if m := numberRe.FindString(raw); m != "" {
		return m
	}
	return raw
}

var numberRe = regexp.MustCompile(`[A-Z]+[\d]+`)

func cleanTitle(raw string) string {
	raw = strings.TrimSpace(raw)
	if i := strings.Index(raw, " | "); i >= 0 {
		raw = raw[:i]
	}
	return raw
}

// extractStudio 从 <title> 后缀【...】取厂牌主名。
func extractStudio(raw string) string {
	raw = strings.TrimSpace(raw)
	if i := strings.Index(raw, " | "); i >= 0 {
		suffix := raw[i+3:]
		if m := bracketRe.FindStringSubmatch(suffix); m != nil {
			return strings.TrimSpace(strings.Split(m[1], "(")[0])
		}
	}
	return ""
}

var bracketRe = regexp.MustCompile(`【(.+?)】`)

func parseMinutes(text string) int {
	if m := digitsRe.FindString(text); m != "" {
		return parser.ParseInt(m)
	}
	return 0
}

var digitsRe = regexp.MustCompile(`\d+`)

func init() {
	provider.Register(Name, New)
}
