package alibaba

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"

	translator "github.com/zzdylan/translator"
)

const (
	hostURL    = "https://translate.alibaba.com"
	apiURL     = "https://translate.alibaba.com/api/translate/text"
	csrfURL    = "https://translate.alibaba.com/api/translate/csrftoken"
	maxTextLen = 5000
)

// Alibaba 使用阿里巴巴翻译引擎实现 Translator 接口。
type Alibaba struct {
	client         *http.Client
	csrfHeaderName string
	csrfToken      string
	langs          []string
	initOnce       sync.Once
	initErr        error
}

func New() *Alibaba {
	return &Alibaba{}
}

func (t *Alibaba) Name() string {
	return "alibaba"
}

func (t *Alibaba) SupportedLanguages() []string {
	return t.langs
}

// fetchLanguages 从阿里巴巴翻译 CDN 动态获取支持的语言列表。
func (t *Alibaba) fetchLanguages(html string) ([]string, error) {
	// 从 HTML 中提取 CDN URL
	cdnRe := regexp.MustCompile(`//lang\.alicdn\.com/mcms/translation-open-portal/(.*?)/translation-open-portal_interface\.json`)
	cdnMatch := cdnRe.FindString(html)
	if cdnMatch == "" {
		return nil, fmt.Errorf("alibaba: failed to extract cdn url from html")
	}
	cdnURL := "https:" + cdnMatch

	// 请求 CDN JSON 文件
	req, err := http.NewRequest("GET", cdnURL, nil)
	if err != nil {
		return nil, fmt.Errorf("alibaba: failed to create cdn request: %w", err)
	}
	req.Header = translator.HostHeaders(hostURL)

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("alibaba: failed to fetch cdn json: %w", err)
	}
	defer resp.Body.Close()

	cdnBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("alibaba: failed to read cdn json: %w", err)
	}
	cdnContent := string(cdnBytes)

	// 提取 en_US 段落中的语言项
	paraRe := regexp.MustCompile(`"en_US":\{(.*?)\},"zh_CN":\{`)
	paraMatch := paraRe.FindStringSubmatch(cdnContent)
	if len(paraMatch) < 2 {
		return nil, fmt.Errorf("alibaba: failed to extract language paragraph from cdn json")
	}

	// 提取语言代码
	itemRe := regexp.MustCompile(`interface\.(.*?)":"(.*?)"`)
	itemMatches := itemRe.FindAllStringSubmatch(paraMatch[1], -1)

	var langs []string
	for _, m := range itemMatches {
		key := m[1]
		value := m[2]
		// 过滤条件与 Python 实现一致：
		// 1. key 长度 <= 3，或长度为 5 且包含 "-"
		// 2. 语言名称不超过 2 个单词
		if (len(key) <= 3 || (len(key) == 5 && strings.Contains(key, "-"))) && len(strings.Fields(value)) <= 2 {
			langs = append(langs, key)
		}
	}

	if len(langs) == 0 {
		return nil, fmt.Errorf("alibaba: no supported languages found")
	}
	sort.Strings(langs)
	return langs, nil
}

func (t *Alibaba) doInit() {
	t.client = translator.NewHTTPClient()

	// 第一步：访问主页获取 cookie 和 HTML
	req, err := http.NewRequest("GET", hostURL, nil)
	if err != nil {
		t.initErr = fmt.Errorf("alibaba: init request error: %w", err)
		return
	}
	req.Header = translator.HostHeaders(hostURL)
	resp, err := t.client.Do(req)
	if err != nil {
		t.initErr = fmt.Errorf("alibaba: init failed: %w", err)
		return
	}
	defer resp.Body.Close()

	// 读取主页 HTML，用于提取语言列表 CDN 地址
	htmlBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.initErr = fmt.Errorf("alibaba: failed to read main page: %w", err)
		return
	}
	html := string(htmlBytes)

	// 动态获取语言列表
	langs, err := t.fetchLanguages(html)
	if err != nil {
		t.initErr = err
		return
	}
	t.langs = langs

	// 第二步：获取 CSRF 令牌（第一次请求，丢弃结果）
	csrfReq1, err := http.NewRequest("GET", csrfURL, nil)
	if err != nil {
		t.initErr = fmt.Errorf("alibaba: csrf request error: %w", err)
		return
	}
	csrfReq1.Header = translator.HostHeaders(hostURL)
	csrfResp1, err := t.client.Do(csrfReq1)
	if err != nil {
		t.initErr = fmt.Errorf("alibaba: first csrf request failed: %w", err)
		return
	}
	csrfResp1.Body.Close()

	// 第三步：获取 CSRF 令牌（第二次请求，使用结果）
	csrfReq2, err := http.NewRequest("GET", csrfURL, nil)
	if err != nil {
		t.initErr = fmt.Errorf("alibaba: csrf request error: %w", err)
		return
	}
	csrfReq2.Header = translator.HostHeaders(hostURL)
	csrfResp2, err := t.client.Do(csrfReq2)
	if err != nil {
		t.initErr = fmt.Errorf("alibaba: second csrf request failed: %w", err)
		return
	}
	defer csrfResp2.Body.Close()

	csrfBody, err := io.ReadAll(csrfResp2.Body)
	if err != nil {
		t.initErr = fmt.Errorf("alibaba: read csrf response error: %w", err)
		return
	}

	var csrfResult map[string]any
	if err := json.Unmarshal(csrfBody, &csrfResult); err != nil {
		t.initErr = fmt.Errorf("alibaba: parse csrf response error: %w", err)
		return
	}

	headerName, ok := csrfResult["headerName"].(string)
	if !ok {
		t.initErr = fmt.Errorf("alibaba: csrf headerName not found, response: %s", string(csrfBody))
		return
	}
	token, ok := csrfResult["token"].(string)
	if !ok {
		t.initErr = fmt.Errorf("alibaba: csrf token not found, response: %s", string(csrfBody))
		return
	}
	t.csrfHeaderName = headerName
	t.csrfToken = token
}

func (t *Alibaba) Translate(text, from, to string) (*translator.TranslateResult, error) {
	t.initOnce.Do(t.doInit)
	if t.initErr != nil {
		return nil, t.initErr
	}

	if len([]rune(text)) > maxTextLen {
		return nil, fmt.Errorf("alibaba: text exceeds %d character limit", maxTextLen)
	}

	// 构建 multipart/form-data 请求体
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	w.WriteField("query", text)
	w.WriteField("srcLang", from)
	w.WriteField("tgtLang", to)
	w.WriteField("_csrf", t.csrfToken)
	w.WriteField("domain", "general")
	w.Close()

	req, err := http.NewRequest("POST", apiURL, &buf)
	if err != nil {
		return nil, fmt.Errorf("alibaba: create request error: %w", err)
	}
	// 设置请求头：Origin、Referer、User-Agent、CSRF 令牌（不含 X-Requested-With）
	origin := translator.ExtractOrigin(hostURL)
	req.Header.Set("Origin", origin)
	req.Header.Set("Referer", hostURL)
	req.Header.Set("User-Agent", translator.UserAgent)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set(t.csrfHeaderName, t.csrfToken)

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("alibaba: translate request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("alibaba: read response error: %w", err)
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("alibaba: parse response error: %w", err)
	}

	data, ok := result["data"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("alibaba: unexpected response format: missing data, response: %s", string(body))
	}

	translated, ok := data["translateText"].(string)
	if !ok {
		return nil, fmt.Errorf("alibaba: unexpected response format: missing translateText, response: %s", string(body))
	}

	return &translator.TranslateResult{
		Text:   translated,
		From:   from,
		To:     to,
		Engine: t.Name(),
	}, nil
}
