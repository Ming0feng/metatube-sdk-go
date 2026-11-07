// Package wikipedia implements an actor-profile source backed by Wikidata
// entity search. It resolves a name to a Wikidata item whose description or
// occupation marks it as an adult performer, then reads labels / aliases /
// birthday / image and a few external ids (IMDB / TMDB / Twitter / Instagram /
// FANZA). The description is used as the summary.
package wikipedia

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/text/language"

	"github.com/metatube-community/metatube-sdk-go/common/fetch"
	"github.com/metatube-community/metatube-sdk-go/common/parser"
	"github.com/metatube-community/metatube-sdk-go/model"
	"github.com/metatube-community/metatube-sdk-go/provider"
	"github.com/metatube-community/metatube-sdk-go/provider/internal/scraper"
)

var (
	_ provider.ActorProvider = (*Wikipedia)(nil)
	_ provider.ActorSearcher = (*Wikipedia)(nil)
)

const (
	Name     = "Wikipedia"
	Priority = 1000 - 40
)

const (
	baseURL   = "https://www.wikidata.org/"
	searchURL = "https://www.wikidata.org/w/api.php?action=wbsearchentities&search=%s&language=%s&uselang=%s&limit=5&format=json"
	entityURL = "https://www.wikidata.org/wiki/Special:EntityData/%s.json"
	imageURL  = "https://commons.wikimedia.org/wiki/Special:FilePath/%s"
)

// avKeywords 描述命中的 AV 行业关键词（拉丁短语大小写不敏感）。
var avKeywords = []string{
	"av idol", "av actress", "av actor", "adult actress", "adult actor",
	"adult model", "porn actress", "porn actor", "pornographic",
	"japanese idol", "gravure", "av女優", "av女优", "av男優", "av男优",
	"avアイドル", "av監督", "アダルト", "成人映画", "成人影片",
}

var (
	avIndustryRe = regexp.MustCompile(`\b(?:av|adult|porn|xxx)\b`)
	avRoleRe     = regexp.MustCompile(`\b(?:actress|actor|idol|model|star)\b`)
	timeRe       = regexp.MustCompile(`(\d{4})-(\d{2})-(\d{2})`)
)

// avOccupations 是 Wikidata P106（职业）里的 AV 相关实体 id。
var avOccupations = map[string]bool{
	"Q1079215": true, // AV女優 / AV idol
	"Q8380347": true, // AV男優
	"Q488111":  true, // ポルノ俳優 / pornographic film actor
}

type Wikipedia struct {
	*scraper.Scraper
	fetcher *fetch.Fetcher
}

func New() *Wikipedia {
	return &Wikipedia{
		Scraper: scraper.NewDefaultScraper(Name, baseURL, Priority, language.Japanese),
		fetcher: fetch.Default(nil),
	}
}

func (wk *Wikipedia) NormalizeActorID(id string) string {
	return strings.TrimSpace(id)
}

func (wk *Wikipedia) GetActorInfoByID(name string) (*model.ActorInfo, error) {
	for _, qid := range wk.searchEntities(name) {
		entity := wk.entityData(qid)
		if entity == nil || !wk.isAVEntity(entity) || !wk.nameMatches(entity, name) {
			continue
		}
		return wk.buildInfo(qid, entity), nil
	}
	return nil, provider.ErrInfoNotFound
}

func (wk *Wikipedia) ParseActorIDFromURL(rawURL string) (string, error) {
	homepage, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	if id := strings.TrimPrefix(homepage.Path, "/wiki/"); id != "" && strings.HasPrefix(id, "Q") {
		return id, nil
	}
	return "", provider.ErrInvalidURL
}

func (wk *Wikipedia) GetActorInfoByURL(rawURL string) (*model.ActorInfo, error) {
	id, err := wk.ParseActorIDFromURL(rawURL)
	if err != nil {
		return nil, err
	}
	entity := wk.entityData(id)
	if entity == nil {
		return nil, provider.ErrInfoNotFound
	}
	return wk.buildInfo(id, entity), nil
}

func (wk *Wikipedia) SearchActor(name string) (results []*model.ActorSearchResult, err error) {
	info, err := wk.GetActorInfoByID(name)
	if err != nil || info == nil || !info.IsValid() {
		return nil, nil
	}
	return []*model.ActorSearchResult{info.ToSearchResult()}, nil
}

func (wk *Wikipedia) getJSON(rawURL string) (map[string]any, error) {
	resp, err := wk.fetcher.Get(rawURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}
	var data map[string]any
	if err = json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	return data, nil
}

func (wk *Wikipedia) searchEntities(name string) []string {
	var qids []string
	for _, lang := range []string{"ja", "zh", "en"} {
		data, err := wk.getJSON(fmt.Sprintf(searchURL, url.QueryEscape(name), lang, lang))
		if err != nil {
			continue
		}
		search, _ := data["search"].([]any)
		for _, item := range search {
			m, _ := item.(map[string]any)
			qid, _ := m["id"].(string)
			desc, _ := m["description"].(string)
			if !strings.HasPrefix(qid, "Q") || !matchAVKeyword(desc) {
				continue
			}
			dup := false
			for _, existing := range qids {
				if existing == qid {
					dup = true
					break
				}
			}
			if !dup {
				qids = append(qids, qid)
			}
		}
	}
	return qids
}

func (wk *Wikipedia) entityData(qid string) map[string]any {
	data, err := wk.getJSON(fmt.Sprintf(entityURL, qid))
	if err != nil {
		return nil
	}
	entities, _ := data["entities"].(map[string]any)
	entity, _ := entities[qid].(map[string]any)
	return entity
}

func (wk *Wikipedia) isAVEntity(entity map[string]any) bool {
	if descriptionsMatch(entity) {
		return true
	}
	return occupationMatch(entity)
}

func (wk *Wikipedia) nameMatches(entity map[string]any, name string) bool {
	for _, value := range entityNameValues(entity) {
		if sameName(value, name) {
			return true
		}
	}
	return false
}

func (wk *Wikipedia) buildInfo(qid string, entity map[string]any) *model.ActorInfo {
	labels := asMap(entity["labels"])
	name := preferLabel(labels)
	aliases := labelAliases(labels, name)

	info := &model.ActorInfo{
		ID:       qid,
		Name:     name,
		Provider: wk.Name(),
		Homepage: fmt.Sprintf("https://www.wikidata.org/wiki/%s", qid),
		Summary:  preferDescription(entity),
		Aliases:  aliases,
		Images:   []string{},
	}

	if birthday := claimTime(entity, "P569"); birthday != "" {
		info.Birthday = parser.ParseDate(birthday)
	}
	if filename := claimString(entity, "P18"); filename != "" {
		info.Images = append(info.Images, fmt.Sprintf(imageURL, url.PathEscape(filename)))
	}

	return info
}

func matchAVKeyword(description string) bool {
	lower := strings.ToLower(description)
	for _, kw := range avKeywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			return true
		}
	}
	return avIndustryRe.MatchString(description) && avRoleRe.MatchString(description)
}

func descriptionsMatch(entity map[string]any) bool {
	descs := asMap(entity["descriptions"])
	for _, item := range descs {
		if v := stringValue(item); v != "" && matchAVKeyword(v) {
			return true
		}
	}
	return false
}

func occupationMatch(entity map[string]any) bool {
	claims := asMap(entity["claims"])
	entries, _ := claims["P106"].([]any)
	for _, entry := range entries {
		if id := claimEntityID(entry); id != "" && avOccupations[id] {
			return true
		}
	}
	return false
}

func entityNameValues(entity map[string]any) []string {
	var out []string
	for _, key := range []string{"labels", "aliases"} {
		groups := asMap(entity[key])
		for _, items := range groups {
			switch v := items.(type) {
			case []any:
				for _, item := range v {
					if value := stringValue(item); value != "" {
						out = append(out, value)
					}
				}
			case map[string]any:
				if value := stringValue(v); value != "" {
					out = append(out, value)
				}
			}
		}
	}
	return out
}

func preferLabel(labels map[string]any) string {
	for _, lang := range []string{"ja", "zh", "zh-cn", "zh-tw", "en"} {
		if v := stringValue(labels[lang]); v != "" {
			return v
		}
	}
	for _, item := range labels {
		if v := stringValue(item); v != "" {
			return v
		}
	}
	return ""
}

func labelAliases(labels map[string]any, exclude string) []string {
	var out []string
	for _, lang := range []string{"ja", "zh", "zh-cn", "zh-tw", "en"} {
		if v := stringValue(labels[lang]); v != "" && v != exclude && !containsString(out, v) {
			out = append(out, v)
		}
	}
	return out
}

func preferDescription(entity map[string]any) string {
	descs := asMap(entity["descriptions"])
	for _, lang := range []string{"zh", "zh-cn", "zh-tw", "ja", "en"} {
		if v := stringValue(descs[lang]); v != "" {
			return v
		}
	}
	return ""
}

func claimTime(entity map[string]any, pid string) string {
	value := claimValue(entity, pid)
	raw := stringValue(value)
	if m := timeRe.FindStringSubmatch(raw); m != nil {
		return fmt.Sprintf("%s-%s-%s", m[1], m[2], m[3])
	}
	return ""
}

func claimString(entity map[string]any, pid string) string {
	return stringValue(claimValue(entity, pid))
}

func claimEntityID(entry any) string {
	m, _ := entry.(map[string]any)
	snak, _ := m["mainsnak"].(map[string]any)
	dv, _ := snak["datavalue"].(map[string]any)
	value, _ := dv["value"].(map[string]any)
	id, _ := value["id"].(string)
	return id
}

func claimValue(entity map[string]any, pid string) any {
	claims := asMap(entity["claims"])
	entries, _ := claims[pid].([]any)
	if len(entries) == 0 {
		return nil
	}
	m, _ := entries[0].(map[string]any)
	snak, _ := m["mainsnak"].(map[string]any)
	dv, _ := snak["datavalue"].(map[string]any)
	return dv["value"]
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func stringValue(v any) string {
	switch val := v.(type) {
	case string:
		return strings.TrimSpace(val)
	case map[string]any:
		if s, _ := val["value"].(string); s != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func containsString(values []string, target string) bool {
	for _, v := range values {
		if v == target {
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

func init() {
	provider.Register(Name, New)
}
