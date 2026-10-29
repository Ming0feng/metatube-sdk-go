package engine

import (
	"regexp"
	"strings"
)

// ContentType 是影片番号的内容类型（用于类型路由归属）。
type ContentType string

const (
	ContentTypeCensored   ContentType = "censored"   // 有码
	ContentTypeUncensored ContentType = "uncensored" // 无码
	ContentTypeFC2        ContentType = "fc2"        // FC2
	ContentTypeWestern    ContentType = "western"    // 欧美
	ContentTypeChinese    ContentType = "chinese"    // 国产
	ContentTypeHentai     ContentType = "hentai"     // 里番 / 成人动画
)

// uncensoredPrefixes 无码片商标前缀（含无码素人站）。
var uncensoredPrefixes = []string{
	"BT-", "CT-", "EMP-", "CCDV-", "CWP-", "CWPBD-", "DSAM-", "DRC-", "DRG-",
	"DRGBD-", "GACHI-", "GACHIG-", "JAV-", "LAF-", "LAFBD-", "HEYZO-", "KTG-",
	"KP-", "KG-", "LLDV-", "MCDV-", "MKD-", "MKBD-", "MMDV-", "NIP-", "PB-",
	"PT-", "QE-", "RED-", "RHJ-", "S2M-", "SKY-", "SKYHD-", "SMD-", "SMBD-",
	"SMDV-", "SSDV-", "SSKP-", "TRG-", "TS-", "XXX-AV-", "YKB-", "BIRD",
	"BOUGA", "H4610-", "C0930-", "H0930-",
}

var (
	sixDigitNumberRe = regexp.MustCompile(`^\d{6}[-_]\d{2,4}$`)                        // 010115-001
	tokyoHotNumberRe = regexp.MustCompile(`^[NK]\d{4}$`)                               // n0716 / k1363
	mdNumberRe       = regexp.MustCompile(`^MD[A-Z-]*\d{4,}`)                          // MD0165
	mkyNumberRe      = regexp.MustCompile(`^MKY-[A-Z]+-\d{3,}`)                        // MKY-NS-001
	westernNumberRe  = regexp.MustCompile(`^[A-Z0-9]{2,}[.-]\d{2}[.-]\d{2}[.-]\d{2}$`) // Studio.YY.MM.DD
	getchuNumberRe   = regexp.MustCompile(`^\d{5,}$`)                                  // Getchu 商品数字 id（里番）
)

// classifyNumber 根据番号判定内容类型。未命中已知形态时按有码处理。
func classifyNumber(number string) ContentType {
	upper := strings.ToUpper(strings.TrimSpace(number))
	if upper == "" {
		return ContentTypeCensored
	}
	if strings.Contains(upper, "FC2") {
		return ContentTypeFC2
	}
	for _, prefix := range uncensoredPrefixes {
		if strings.HasPrefix(upper, prefix) {
			return ContentTypeUncensored
		}
	}
	if strings.HasPrefix(upper, "S2M") || strings.HasPrefix(upper, "MCB3D") {
		return ContentTypeUncensored
	}
	if sixDigitNumberRe.MatchString(upper) || tokyoHotNumberRe.MatchString(upper) {
		return ContentTypeUncensored
	}
	if mdNumberRe.MatchString(upper) && !strings.Contains(upper, "MDVR") {
		return ContentTypeChinese
	}
	if mkyNumberRe.MatchString(upper) {
		return ContentTypeChinese
	}
	if westernNumberRe.MatchString(upper) {
		return ContentTypeWestern
	}
	if getchuNumberRe.MatchString(upper) {
		return ContentTypeHentai
	}
	return ContentTypeCensored
}

// 类型路由：综合索引总是参与搜索，类型专属站只在对应类型时参与。
var (
	generalProviders = []string{
		"JavDB", "JavBus", "JAVFREE", "AVBASE", "JAV321", "FREEJAVBT",
	}
	censoredProviders = []string{
		"FANZA", "SOD", "MGS", "FALENO", "DAHLIA", "DUGA", "Pcolle", "AVE", "OFFICIAL",
	}
	uncensoredProviders = []string{
		"1Pondo", "10musume", "Caribbeancom", "CaribbeancomPR", "HeyDouga", "HEYZO",
		"C0930", "H0930", "H4610", "KIN8", "MURAMURA", "PACOPACOMAMA", "TOKYO-HOT",
		"Gcolle", "MYWIFE", "AVSOX",
	}
	fc2Providers = []string{
		"FC2", "fc2hub", "FC2PPVDB",
	}
	chineseProviders = []string{
		"IQQTV", "AiRav",
	}
	westernProviders = []string{}
	hentaiProviders  = []string{
		"Getchu",
	}
)

// allowedProviderNames 返回搜索 keyword 时应参与的 provider 名集合。
// 未列出的 provider（如被禁用的优先级 <= 0）不参与本次搜索。
func allowedProviderNames(keyword string) map[string]struct{} {
	allowed := make(map[string]struct{}, 32)
	add := func(names ...string) {
		for _, name := range names {
			allowed[name] = struct{}{}
		}
	}
	add(generalProviders...)
	switch classifyNumber(keyword) {
	case ContentTypeUncensored:
		add(uncensoredProviders...)
	case ContentTypeFC2:
		add(fc2Providers...)
		add(chineseProviders...) // AiRav 也收录 FC2
	case ContentTypeWestern:
		add(westernProviders...)
	case ContentTypeChinese:
		add(chineseProviders...)
	case ContentTypeHentai:
		add(hentaiProviders...)
		add(chineseProviders...) // 中文站也收录里番动画
	default: // censored
		add(censoredProviders...)
		add(chineseProviders...) // 中文站对有码片提供中文标题/剧情补强
	}
	return allowed
}
