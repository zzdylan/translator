package bing

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"

	translator "github.com/zzdylan/translator"
)

const (
	hostURL    = "https://cn.bing.com/Translator"
	apiURL     = "https://cn.bing.com/ttranslatev3"
	maxTextLen = 1000
)

// Bing 使用必应翻译引擎实现 Translator 接口。
type Bing struct {
	client  *http.Client
	langs   []string
	ig      string // IG 参数
	iid     string // IID 参数
	key     string // AbusePreventionHelper key
	token   string // AbusePreventionHelper token
	once    sync.Once
	initErr error
}

func New() *Bing {
	return &Bing{}
}

func (b *Bing) Name() string {
	return "bing"
}

func (b *Bing) SupportedLanguages() []string {
	return b.langs
}

func (b *Bing) init() error {
	b.client = translator.NewHTTPClient()

	// 获取主页 HTML
	req, err := http.NewRequest("GET", hostURL, nil)
	if err != nil {
		return fmt.Errorf("bing: 创建初始化请求失败: %w", err)
	}
	req.Header = translator.HostHeaders(hostURL)

	resp, err := b.client.Do(req)
	if err != nil {
		return fmt.Errorf("bing: 获取主页失败: %w", err)
	}
	defer resp.Body.Close()

	htmlBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("bing: 读取主页失败: %w", err)
	}
	html := string(htmlBytes)

	// 提取 IG 参数
	igRe := regexp.MustCompile(`IG:"(.*?)"`)
	igMatch := igRe.FindStringSubmatch(html)
	if len(igMatch) < 2 {
		return fmt.Errorf("bing: 无法从主页提取 IG 参数")
	}
	b.ig = igMatch[1]

	// 提取 data-iid 属性
	iidRe := regexp.MustCompile(`data-iid="(.*?)"`)
	iidMatch := iidRe.FindStringSubmatch(html)
	if len(iidMatch) < 2 {
		return fmt.Errorf("bing: 无法从主页提取 data-iid 参数")
	}
	b.iid = iidMatch[1]

	// 提取 AbusePreventionHelper 参数（key 和 token）
	tkRe := regexp.MustCompile(`var params_AbusePreventionHelper\s*=\s*\[(.*?)\];`)
	tkMatch := tkRe.FindStringSubmatch(html)
	if len(tkMatch) < 2 {
		return fmt.Errorf("bing: 无法从主页提取 AbusePreventionHelper 参数")
	}
	// 解析数组内容，格式为: key,token,timeout
	parts := strings.Split(tkMatch[1], ",")
	if len(parts) < 2 {
		return fmt.Errorf("bing: AbusePreventionHelper 参数格式不正确")
	}
	b.key = strings.TrimSpace(parts[0])
	b.token = strings.Trim(strings.TrimSpace(parts[1]), `"`)

	// 从 HTML 提取语言列表（<option ... value="xx">，可能包含 aria-label 属性）
	langRe := regexp.MustCompile(`<option[^>]*\svalue="([a-zA-Z][a-zA-Z0-9-]*)"`)
	langMatches := langRe.FindAllStringSubmatch(html, -1)
	if len(langMatches) == 0 {
		return fmt.Errorf("bing: 无法从主页提取语言列表")
	}

	// 排除非语言代码的值
	excludeSet := map[string]bool{
		"auto-detect": true,
		"Standard":    true,
		"Casual":      true,
		"Formal":      true,
	}
	langSet := make(map[string]struct{})
	for _, m := range langMatches {
		lang := m[1]
		if lang != "" && !excludeSet[lang] {
			langSet[lang] = struct{}{}
		}
	}

	var langs []string
	for lang := range langSet {
		langs = append(langs, lang)
	}
	if len(langs) == 0 {
		return fmt.Errorf("bing: 未找到支持的语言")
	}
	sort.Strings(langs)
	b.langs = langs

	return nil
}

func (b *Bing) Translate(text, from, to string) (*translator.TranslateResult, error) {
	b.once.Do(func() {
		b.initErr = b.init()
	})
	if b.initErr != nil {
		return nil, b.initErr
	}

	if len([]rune(text)) > maxTextLen {
		return nil, fmt.Errorf("bing: 文本超过 %d 字符限制", maxTextLen)
	}

	// 语言代码映射（与 Python check_language 一致）
	fromLang := mapLang(from)
	toLang := mapLang(to)

	// 构造 form-data
	form := url.Values{}
	form.Set("text", text)
	form.Set("fromLang", fromLang)
	form.Set("to", toLang)
	form.Set("tryFetchingGenderDebiasedTranslations", "true")
	form.Set("token", b.token)
	form.Set("key", b.key)

	// 构造 API URL
	apiURLWithParams := fmt.Sprintf("%s?isVertical=1&&IG=%s&IID=%s", apiURL, b.ig, b.iid)

	req, err := http.NewRequest("POST", apiURLWithParams, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("bing: 创建翻译请求失败: %w", err)
	}
	req.Header = translator.HostHeaders(hostURL)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bing: 翻译请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("bing: 读取响应失败: %w", err)
	}

	// 解析 JSON 响应
	var result []map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("bing: 解析响应失败: %w (body: %s)", err, string(body))
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("bing: 响应数组为空, response: %s", string(body))
	}

	translations, ok := result[0]["translations"].([]any)
	if !ok || len(translations) == 0 {
		return nil, fmt.Errorf("bing: 响应中缺少 translations 字段, response: %s", string(body))
	}

	firstTranslation, ok := translations[0].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("bing: translations 格式不正确, response: %s", string(body))
	}

	translatedText, ok := firstTranslation["text"].(string)
	if !ok {
		return nil, fmt.Errorf("bing: 翻译结果中缺少 text 字段, response: %s", string(body))
	}

	return &translator.TranslateResult{
		Text:   translatedText,
		From:   from,
		To:     to,
		Engine: b.Name(),
	}, nil
}

// mapLang 将通用语言代码映射为 Bing 的语言代码。
func mapLang(lang string) string {
	switch lang {
	case "auto":
		return "auto-detect"
	case "zh", "zh-CN", "zh-cn", "zh-CHS", "cn":
		return "zh-Hans"
	case "zh-TW", "zh-tw", "zh-CHT":
		return "zh-Hant"
	default:
		return lang
	}
}
