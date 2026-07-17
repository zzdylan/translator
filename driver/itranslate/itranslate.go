package itranslate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"sync"

	translator "github.com/zzdylan/translator"
)

const (
	hostURL     = "https://itranslate.com/translate"
	apiURL      = "https://web-api.itranslateapp.com/v3/texts/translate"
	manifestURL = "https://itranslate-webapp-production.web.app/manifest.json"
	maxTextLen  = 1000
)

// ITranslate 使用 iTranslate 翻译引擎实现 Translator 接口。
type ITranslate struct {
	client *http.Client
	langs  []string
	apiKey string
	once   sync.Once
	initErr error
}

func New() *ITranslate {
	return &ITranslate{}
}

func (t *ITranslate) Name() string {
	return "itranslate"
}

func (t *ITranslate) SupportedLanguages() []string {
	return t.langs
}

func (t *ITranslate) init() error {
	t.client = translator.NewHTTPClient()

	// 访问主页获取 cookie
	req, err := http.NewRequest("GET", hostURL, nil)
	if err != nil {
		return fmt.Errorf("itranslate: 创建主页请求失败: %w", err)
	}
	req.Header = translator.HostHeaders(hostURL)

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("itranslate: 获取主页失败: %w", err)
	}
	resp.Body.Close()

	// 获取 manifest.json 以找到 main.js 路径
	manifestReq, err := http.NewRequest("GET", manifestURL, nil)
	if err != nil {
		return fmt.Errorf("itranslate: 创建 manifest 请求失败: %w", err)
	}
	manifestReq.Header = translator.HostHeaders(hostURL)

	manifestResp, err := t.client.Do(manifestReq)
	if err != nil {
		return fmt.Errorf("itranslate: 获取 manifest 失败: %w", err)
	}
	defer manifestResp.Body.Close()

	var manifest map[string]any
	if err := json.NewDecoder(manifestResp.Body).Decode(&manifest); err != nil {
		return fmt.Errorf("itranslate: 解析 manifest 失败: %w", err)
	}

	mainJSPath, ok := manifest["main.js"].(string)
	if !ok {
		return fmt.Errorf("itranslate: manifest 中缺少 main.js 路径")
	}

	// 获取 main.js 文件
	jsReq, err := http.NewRequest("GET", mainJSPath, nil)
	if err != nil {
		return fmt.Errorf("itranslate: 创建 JS 请求失败: %w", err)
	}
	jsReq.Header = translator.HostHeaders(hostURL)

	jsResp, err := t.client.Do(jsReq)
	if err != nil {
		return fmt.Errorf("itranslate: 获取 JS 文件失败: %w", err)
	}
	defer jsResp.Body.Close()

	jsBytes, err := io.ReadAll(jsResp.Body)
	if err != nil {
		return fmt.Errorf("itranslate: 读取 JS 文件失败: %w", err)
	}
	jsContent := string(jsBytes)

	// 提取 API-KEY
	apiKeyRe := regexp.MustCompile(`"API-KEY":"(.*?)"`)
	apiKeyMatch := apiKeyRe.FindStringSubmatch(jsContent)
	if len(apiKeyMatch) < 2 {
		return fmt.Errorf("itranslate: 无法从 JS 中提取 API-KEY")
	}
	t.apiKey = apiKeyMatch[1]

	// 提取语言列表
	langBlockRe := regexp.MustCompile(`\[{dialect:"auto",(.*?)}\]`)
	langBlockMatch := langBlockRe.FindString(jsContent)
	if langBlockMatch == "" {
		return fmt.Errorf("itranslate: 无法从 JS 中提取语言列表")
	}

	dialectRe := regexp.MustCompile(`dialect:"(.*?)"`)
	dialectMatches := dialectRe.FindAllStringSubmatch(langBlockMatch, -1)
	if len(dialectMatches) == 0 {
		return fmt.Errorf("itranslate: 无法解析语言方言列表")
	}

	langSet := make(map[string]struct{})
	for _, m := range dialectMatches {
		lang := m[1]
		if lang != "auto" {
			langSet[lang] = struct{}{}
		}
	}

	var langs []string
	for lang := range langSet {
		langs = append(langs, lang)
	}
	if len(langs) == 0 {
		return fmt.Errorf("itranslate: 未找到支持的语言")
	}
	sort.Strings(langs)
	t.langs = langs

	return nil
}

func (t *ITranslate) Translate(text, from, to string) (*translator.TranslateResult, error) {
	t.once.Do(func() {
		t.initErr = t.init()
	})
	if t.initErr != nil {
		return nil, t.initErr
	}

	if len([]rune(text)) > maxTextLen {
		return nil, fmt.Errorf("itranslate: 文本超过 %d 字符限制", maxTextLen)
	}

	// 语言代码映射（与 Python check_language 一致）
	fromDialect := mapLang(from)
	toDialect := mapLang(to)

	// 构造请求 JSON
	payload := map[string]any{
		"source": map[string]any{
			"dialect": fromDialect,
			"text":    text,
			"with":    []string{"synonyms"},
		},
		"target": map[string]any{
			"dialect": toDialect,
		},
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("itranslate: 序列化请求失败: %w", err)
	}

	req, err := http.NewRequest("POST", apiURL, bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("itranslate: 创建翻译请求失败: %w", err)
	}
	req.Header = translator.APIHeaders(hostURL, "application/json")
	req.Header.Set("API-KEY", t.apiKey)

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("itranslate: 翻译请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("itranslate: 读取响应失败: %w", err)
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("itranslate: 解析响应失败: %w (body: %s)", err, string(body))
	}

	target, ok := result["target"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("itranslate: 响应中缺少 target 字段, response: %s", string(body))
	}

	translatedText, ok := target["text"].(string)
	if !ok {
		return nil, fmt.Errorf("itranslate: target 中缺少 text 字段, response: %s", string(body))
	}

	return &translator.TranslateResult{
		Text:   translatedText,
		From:   from,
		To:     to,
		Engine: t.Name(),
	}, nil
}

// mapLang 将通用语言代码映射为 iTranslate 的方言代码。
func mapLang(lang string) string {
	switch lang {
	case "zh", "zh-CHS", "zh-Hans", "cn":
		return "zh-CN"
	case "en":
		return "en-US"
	default:
		return lang
	}
}
