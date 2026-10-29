package avbase

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path"
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
	_ provider.ActorProvider = (*AVBaseActor)(nil)
	_ provider.ActorSearcher = (*AVBaseActor)(nil)
)

const (
	ActorProviderName = "AVBASEActor"
	actorPriority     = 1000 - 4
	actorURL          = "https://www.avbase.net/talents/%s"
)

type AVBaseActor struct {
	*scraper.Scraper
}

func NewActor() *AVBaseActor {
	return &AVBaseActor{Scraper: scraper.NewDefaultScraper(ActorProviderName, baseURL, actorPriority, language.Japanese)}
}

// talentResponse 对应 avbase `/talents/{name}` 页 __NEXT_DATA__ 里的 talent 对象。
type talentResponse struct {
	ID      int `json:"id"`
	Primary struct {
		Name     string `json:"name"`
		Note     string `json:"note"`
		ImageURL string `json:"image_url"`
	} `json:"primary"`
	Actors []struct {
		Name string `json:"name"`
	} `json:"actors"`
	Meta struct {
		BasicInfo struct {
			Birthday    string `json:"birthday"`
			Prefectures string `json:"prefectures"`
			Height      string `json:"height"`
			Bust        string `json:"bust"`
			Waist       string `json:"waist"`
			Hip         string `json:"hip"`
			Cup         string `json:"cup"`
		} `json:"basic_info"`
	} `json:"meta"`
}

func (aa *AVBaseActor) NormalizeActorID(id string) string {
	return strings.TrimSpace(id)
}

func (aa *AVBaseActor) GetActorInfoByID(id string) (*model.ActorInfo, error) {
	return aa.GetActorInfoByURL(fmt.Sprintf(actorURL, url.PathEscape(id)))
}

func (aa *AVBaseActor) ParseActorIDFromURL(rawURL string) (string, error) {
	homepage, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	if id := path.Base(homepage.Path); id != "" && id != "/" && id != "." {
		return id, nil
	}
	return "", provider.ErrInvalidURL
}

func (aa *AVBaseActor) GetActorInfoByURL(rawURL string) (info *model.ActorInfo, err error) {
	info = &model.ActorInfo{
		Provider: aa.Name(),
		Homepage: rawURL,
		Aliases:  []string{},
		Images:   []string{},
	}

	c := aa.ClonedCollector()

	c.OnXML(`//*[@id="__NEXT_DATA__"]`, func(e *colly.XMLElement) {
		data := struct {
			PageProps struct {
				Talent talentResponse `json:"talent"`
			} `json:"pageProps"`
		}{}
		if json.NewDecoder(strings.NewReader(e.Text)).Decode(&data) != nil {
			return
		}
		talent := data.PageProps.Talent
		if talent.Primary.Name == "" {
			return
		}

		info.ID = strconv.Itoa(talent.ID)
		info.Name = strings.TrimSpace(talent.Primary.Name)
		if talent.Primary.ImageURL != "" {
			info.Images = append(info.Images, talent.Primary.ImageURL)
		}
		for _, actor := range talent.Actors {
			if name := strings.TrimSpace(actor.Name); name != "" && name != info.Name {
				info.Aliases = append(info.Aliases, name)
			}
		}

		basic := talent.Meta.BasicInfo
		if time.Time(info.Birthday).IsZero() {
			info.Birthday = parser.ParseDate(basic.Birthday)
		}
		info.Height = atoi(basic.Height)
		info.CupSize = strings.ToUpper(strings.TrimSpace(basic.Cup))
		if b, w, h := atoi(basic.Bust), atoi(basic.Waist), atoi(basic.Hip); b != 0 && w != 0 && h != 0 {
			info.Measurements = fmt.Sprintf("B:%d / W:%d / H:%d", b, w, h)
		}
	})

	if err = c.Visit(rawURL); err != nil {
		return nil, err
	}
	if info.Name == "" {
		return nil, provider.ErrInfoNotFound
	}
	return
}

func (aa *AVBaseActor) SearchActor(name string) (results []*model.ActorSearchResult, err error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil
	}
	// 站点无演员检索入口，直接按名字构造人物页 URL。
	homepage := fmt.Sprintf(actorURL, url.PathEscape(name))
	results = append(results, &model.ActorSearchResult{
		ID:       name,
		Name:     name,
		Provider: aa.Name(),
		Homepage: homepage,
	})
	return results, nil
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

func init() {
	provider.Register(ActorProviderName, NewActor)
}
