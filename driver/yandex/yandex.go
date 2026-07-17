package yandex

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"

	translator "github.com/zzdylan/translator"
)

const (
	baseURL      = "https://browser.translate.yandex.net/api/v1/tr.json"
	translateURL = baseURL + "/translate"
	detectURL    = baseURL + "/detect"
	homeURL      = "https://www.youtube.com"
	yaBrowserUA  = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 YaBrowser/24.1.5.825 Yowser/2.5 Safari/537.36"
	maxTextLen   = 10000
)

// Yandex 使用 Yandex 浏览器翻译引擎实现 Translator 接口。
type Yandex struct {
	client   *http.Client
	langs    []string
	initOnce sync.Once
	initErr  error
}

func New() *Yandex {
	return &Yandex{}
}

func (t *Yandex) Name() string {
	return "yandex"
}

func (t *Yandex) SupportedLanguages() []string {
	return t.langs
}

// fetchLanguages 从 API 获取支持的语言列表。
func (t *Yandex) fetchLanguages() ([]string, error) {
	form := url.Values{}
	form.Set("maxRetryCount", "2")
	form.Set("fetchAbortTimeout", "500")

	req, err := http.NewRequest("POST", baseURL+"/getLangs?srv=browser_video_translation", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("yandex: fetch languages request error: %w", err)
	}
	t.setHeaders(req)

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("yandex: fetch languages failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("yandex: read languages response error: %w", err)
	}

	var result struct {
		Dirs []string `json:"dirs"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("yandex: parse languages response error: %w", err)
	}

	// 从语言对中提取唯一的语言代码
	seen := make(map[string]bool)
	var langs []string
	for _, pair := range result.Dirs {
		parts := strings.SplitN(pair, "-", 2)
		for _, code := range parts {
			if code != "" && !seen[code] {
				seen[code] = true
				langs = append(langs, code)
			}
		}
	}

	// 与 Python 实现保持一致：API 可能不返回 zh，需手动补充
	if !seen["zh"] {
		langs = append(langs, "zh")
	}

	sort.Strings(langs)
	return langs, nil
}

func (t *Yandex) doInit() {
	t.client = translator.NewHTTPClient()

	// 动态获取支持的语言列表
	langs, err := t.fetchLanguages()
	if err != nil {
		t.initErr = err
		return
	}
	t.langs = langs
}

func (t *Yandex) setHeaders(req *http.Request) {
	req.Header.Set("Origin", homeURL)
	req.Header.Set("Referer", homeURL)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Set("User-Agent", yaBrowserUA)
}

func (t *Yandex) detectLanguage(text string) (string, error) {
	params := url.Values{}
	params.Set("srv", "browser_video_translation")
	params.Set("text", text)

	form := url.Values{}
	form.Set("maxRetryCount", "2")
	form.Set("fetchAbortTimeout", "500")

	req, err := http.NewRequest("POST", detectURL+"?"+params.Encode(), strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("yandex: detect request error: %w", err)
	}
	t.setHeaders(req)

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("yandex: detect request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("yandex: read detect response error: %w", err)
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("yandex: parse detect response error: %w", err)
	}

	lang, ok := result["lang"].(string)
	if !ok {
		return "", fmt.Errorf("yandex: detect response missing lang, response: %s", string(body))
	}
	return lang, nil
}

func (t *Yandex) Translate(text, from, to string) (*translator.TranslateResult, error) {
	t.initOnce.Do(t.doInit)
	if t.initErr != nil {
		return nil, t.initErr
	}

	if len([]rune(text)) > maxTextLen {
		return nil, fmt.Errorf("yandex: text exceeds %d character limit", maxTextLen)
	}

	sourceLang := from
	if from == "auto" {
		detected, err := t.detectLanguage(text)
		if err != nil {
			return nil, err
		}
		sourceLang = detected
	}

	langPair := sourceLang + "-" + to

	params := url.Values{}
	params.Set("srv", "browser_video_translation")
	params.Set("text", text)
	params.Set("lang", langPair)

	form := url.Values{}
	form.Set("maxRetryCount", "2")
	form.Set("fetchAbortTimeout", "500")

	req, err := http.NewRequest("POST", translateURL+"?"+params.Encode(), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("yandex: create request error: %w", err)
	}
	t.setHeaders(req)

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("yandex: translate request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("yandex: read response error: %w", err)
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("yandex: parse response error: %w", err)
	}

	textArr, ok := result["text"].([]any)
	if !ok || len(textArr) == 0 {
		return nil, fmt.Errorf("yandex: unexpected response format: missing text, response: %s", string(body))
	}

	translated, ok := textArr[0].(string)
	if !ok {
		return nil, fmt.Errorf("yandex: unexpected response format: text[0] not a string, response: %s", string(body))
	}

	return &translator.TranslateResult{
		Text:   translated,
		From:   sourceLang,
		To:     to,
		Engine: t.Name(),
	}, nil
}
