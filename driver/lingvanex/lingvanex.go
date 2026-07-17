package lingvanex

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
	hostURL      = "https://lingvanex.com/en/translate/"
	translateURL = "https://api-b2b.backenster.com/b1/api/v3/translate/?client=site&feature=seo_text&lang_pair=en_te"
	maxTextLen   = 1000
)

var tokenRe = regexp.MustCompile(`const API_BEARER_TOKEN = "(.*?)"`)

// Lingvanex 使用 Lingvanex 翻译引擎实现 Translator 接口。
type Lingvanex struct {
	client    *http.Client
	authToken string
	langs     []string
	initOnce  sync.Once
	initErr   error
}

func New() *Lingvanex {
	return &Lingvanex{}
}

func (t *Lingvanex) Name() string {
	return "lingvanex"
}

func (t *Lingvanex) SupportedLanguages() []string {
	return t.langs
}

// fetchLanguages 从 API 获取支持的语言列表。
func (t *Lingvanex) fetchLanguages() ([]string, error) {
	req, err := http.NewRequest("GET", "https://api-b2b.backenster.com/b1/api/v3/getLanguages?platform=dp", nil)
	if err != nil {
		return nil, fmt.Errorf("lingvanex: fetch languages request error: %w", err)
	}
	req.Header = translator.APIHeaders(hostURL, "")
	req.Header.Set("Authorization", t.authToken)

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("lingvanex: fetch languages failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("lingvanex: read languages response error: %w", err)
	}

	var result struct {
		Result []struct {
			FullCode string `json:"full_code"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("lingvanex: parse languages response error: %w", err)
	}

	// 去重并排序
	seen := make(map[string]bool)
	var langs []string
	for _, item := range result.Result {
		if item.FullCode != "" && !seen[item.FullCode] {
			seen[item.FullCode] = true
			langs = append(langs, item.FullCode)
		}
	}
	sort.Strings(langs)
	return langs, nil
}

func (t *Lingvanex) doInit() {
	t.client = translator.NewHTTPClient()

	// 访问主页提取 API_BEARER_TOKEN
	req, err := http.NewRequest("GET", hostURL, nil)
	if err != nil {
		t.initErr = fmt.Errorf("lingvanex: init request error: %w", err)
		return
	}
	req.Header = translator.HostHeaders(hostURL)
	resp, err := t.client.Do(req)
	if err != nil {
		t.initErr = fmt.Errorf("lingvanex: init failed: %w", err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.initErr = fmt.Errorf("lingvanex: read response error: %w", err)
		return
	}

	matches := tokenRe.FindStringSubmatch(string(body))
	if len(matches) < 2 {
		t.initErr = fmt.Errorf("lingvanex: API_BEARER_TOKEN not found in page")
		return
	}
	t.authToken = matches[1]

	// 动态获取支持的语言列表
	langs, err := t.fetchLanguages()
	if err != nil {
		t.initErr = err
		return
	}
	t.langs = langs
}

func (t *Lingvanex) Translate(text, from, to string) (*translator.TranslateResult, error) {
	t.initOnce.Do(t.doInit)
	if t.initErr != nil {
		return nil, t.initErr
	}

	if len([]rune(text)) > maxTextLen {
		return nil, fmt.Errorf("lingvanex: text exceeds %d character limit", maxTextLen)
	}

	reqFrom := from
	if from == "auto" {
		reqFrom = "zh-Hans_CN"
	}

	form := url.Values{}
	form.Set("from", reqFrom)
	form.Set("to", to)
	form.Set("text", text)
	form.Set("platform", "dp")

	req, err := http.NewRequest("POST", translateURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("lingvanex: create request error: %w", err)
	}
	req.Header = translator.APIHeaders(hostURL, "")
	req.Header.Set("Authorization", t.authToken)

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("lingvanex: translate request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("lingvanex: read response error: %w", err)
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("lingvanex: parse response error: %w", err)
	}

	translated, ok := result["result"].(string)
	if !ok {
		return nil, fmt.Errorf("lingvanex: unexpected response format: missing result, response: %s", string(body))
	}

	// 清理换行后的多余空格
	translated = strings.ReplaceAll(translated, "\n ", "\n")

	return &translator.TranslateResult{
		Text:   translated,
		From:   from,
		To:     to,
		Engine: t.Name(),
	}, nil
}
