// Package reverso 提供 Reverso 翻译驱动。
//
// 注意：Reverso API（api.reverso.net）受 Cloudflare Managed Challenge 保护，
// Python 实现使用 cloudscraper（内置 JS 引擎）绕过，
// 但目前 Go 暂时没有验证过的可行方案，该驱动可能无法正常工作。
package reverso

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"

	translator "github.com/zzdylan/translator"
)

const (
	hostURL    = "https://www.reverso.net/text-translation"
	apiURL     = "https://api.reverso.net/translate/v1/translation"
	langURL    = "https://cdn.reverso.net/trans/v2.22.8/main.js"
	maxTextLen = 2000
)

// Reverso 使用 Reverso 翻译引擎实现 Translator 接口。
type Reverso struct {
	client         *http.Client
	langs          []string
	decryptLangMap map[string]string // 显示语言代码 -> API 语言代码
	once           sync.Once
	initErr        error
}

func New() *Reverso {
	return &Reverso{}
}

func (r *Reverso) Name() string {
	return "reverso"
}

func (r *Reverso) SupportedLanguages() []string {
	return r.langs
}

func (r *Reverso) init() error {
	r.client = translator.NewHTTPClient()

	// 访问主页获取 cookie
	req, err := http.NewRequest("GET", hostURL, nil)
	if err != nil {
		return fmt.Errorf("reverso: 创建主页请求失败: %w", err)
	}
	req.Header = translator.HostHeaders(hostURL)

	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("reverso: 获取主页失败: %w", err)
	}
	resp.Body.Close()

	// 获取语言 JS 文件
	jsReq, err := http.NewRequest("GET", langURL, nil)
	if err != nil {
		return fmt.Errorf("reverso: 创建 JS 请求失败: %w", err)
	}
	jsReq.Header = translator.HostHeaders(hostURL)

	jsResp, err := r.client.Do(jsReq)
	if err != nil {
		return fmt.Errorf("reverso: 获取 JS 文件失败: %w", err)
	}
	defer jsResp.Body.Close()

	jsBytes, err := io.ReadAll(jsResp.Body)
	if err != nil {
		return fmt.Errorf("reverso: 读取 JS 文件失败: %w", err)
	}
	jsContent := string(jsBytes)

	// 提取语言映射：={eng:(.*?)}
	// JS 中的格式类似: ={eng:"en",fra:"fr",deu:"de",...}
	langDictRe := regexp.MustCompile(`=\{eng:(.*?)\}`)
	langDictMatch := langDictRe.FindStringSubmatch(jsContent)
	if len(langDictMatch) < 2 {
		return fmt.Errorf("reverso: 无法从 JS 中提取语言映射")
	}

	// 解析语言映射
	rawDict := "{eng:" + langDictMatch[1] + "}"
	r.decryptLangMap, r.langs = parseLangDict(rawDict)

	if len(r.langs) == 0 {
		return fmt.Errorf("reverso: 未找到支持的语言")
	}

	return nil
}

// parseLangDict 从 JS 对象字符串中解析语言映射。
// 格式如: {eng:"en",fra:"fr",...}
// 返回 decryptMap（API值->JS键）和排序后的语言列表（API值）。
func parseLangDict(raw string) (map[string]string, []string) {
	// 提取所有 key:"value" 对
	pairRe := regexp.MustCompile(`(\w+):"([^"]*)"`)
	pairs := pairRe.FindAllStringSubmatch(raw, -1)

	decryptMap := make(map[string]string)
	langSet := make(map[string]struct{})

	for _, p := range pairs {
		jsKey := p[1]    // 如 eng, fra
		apiVal := p[2]   // 如 en, fr
		decryptMap[apiVal] = jsKey
		langSet[apiVal] = struct{}{}
	}

	var langs []string
	for lang := range langSet {
		langs = append(langs, lang)
	}
	sort.Strings(langs)

	return decryptMap, langs
}

func (r *Reverso) Translate(text, from, to string) (*translator.TranslateResult, error) {
	r.once.Do(func() {
		r.initErr = r.init()
	})
	if r.initErr != nil {
		return nil, r.initErr
	}

	if len([]rune(text)) > maxTextLen {
		return nil, fmt.Errorf("reverso: 文本超过 %d 字符限制", maxTextLen)
	}

	// Reverso 不支持 auto，默认使用 zh
	fromLang := from
	if fromLang == "auto" {
		fromLang = "zh"
	}

	// 将显示语言代码转换为 API 语言代码
	apiFrom, ok := r.decryptLangMap[fromLang]
	if !ok {
		return nil, fmt.Errorf("reverso: 不支持的源语言: %s", fromLang)
	}
	apiTo, ok := r.decryptLangMap[to]
	if !ok {
		return nil, fmt.Errorf("reverso: 不支持的目标语言: %s", to)
	}

	// 构造请求 JSON
	payload := map[string]any{
		"format": "text",
		"from":   apiFrom,
		"to":     apiTo,
		"input":  text,
		"options": map[string]any{
			"contextResults":    "true",
			"languageDetection": "true",
			"sentenceSplitter":  "true",
			"origin":            "translation.web",
		},
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("reverso: 序列化请求失败: %w", err)
	}

	req, err := http.NewRequest("POST", apiURL, bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("reverso: 创建翻译请求失败: %w", err)
	}
	req.Header = translator.APIHeaders(hostURL, "application/json")
	req.Header.Set("X-Reverso-Origin", "translation.web")

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reverso: 翻译请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reverso: 读取响应失败: %w", err)
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("reverso: 解析响应失败: %w (body: %s)", err, string(body))
	}

	// 提取翻译结果
	translationArr, ok := result["translation"].([]any)
	if !ok {
		return nil, fmt.Errorf("reverso: 响应中缺少 translation 字段, response: %s", string(body))
	}

	var sb strings.Builder
	for _, item := range translationArr {
		if s, ok := item.(string); ok {
			sb.WriteString(s)
		}
	}

	return &translator.TranslateResult{
		Text:   sb.String(),
		From:   from,
		To:     to,
		Engine: r.Name(),
	}, nil
}
