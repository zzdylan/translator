package modernmt

import (
	"bytes"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"sync"
	"time"

	translator "github.com/zzdylan/translator"
)

const (
	hostURL    = "https://www.modernmt.com/translate"
	apiURL     = "https://webapi.modernmt.com/translate"
	webkey     = "webkey_E3sTuMjpP8Jez49GcYpDVH7r"
	maxTextLen = 5000
)

// ModernMT 使用 ModernMT 翻译引擎实现 Translator 接口。
type ModernMT struct {
	client  *http.Client
	langs   []string
	once    sync.Once
	initErr error
}

func New() *ModernMT {
	return &ModernMT{}
}

func (m *ModernMT) Name() string {
	return "modernmt"
}

func (m *ModernMT) SupportedLanguages() []string {
	return m.langs
}

// fetchLanguages 从 ModernMT 的 JS bundle 中动态获取支持的语言列表。
func (m *ModernMT) fetchLanguages() ([]string, error) {
	bundleURL := "https://www.modernmt.com/scripts/app.bundle.js"

	req, err := http.NewRequest("GET", bundleURL, nil)
	if err != nil {
		return nil, fmt.Errorf("modernmt: failed to create bundle request: %w", err)
	}
	req.Header = translator.HostHeaders(hostURL)

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("modernmt: failed to fetch bundle js: %w", err)
	}
	defer resp.Body.Close()

	jsBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("modernmt: failed to read bundle js: %w", err)
	}
	jsContent := string(jsBytes)

	// 从 JS 中提取单引号包裹的 JSON 对象
	jsonRe := regexp.MustCompile(`'(\{.*?\})'`)
	jsonMatch := jsonRe.FindStringSubmatch(jsContent)
	if len(jsonMatch) < 2 {
		return nil, fmt.Errorf("modernmt: failed to extract language json from bundle")
	}

	// 解析 JSON，提取所有键作为语言代码
	var langMap map[string]any
	if err := json.Unmarshal([]byte(jsonMatch[1]), &langMap); err != nil {
		return nil, fmt.Errorf("modernmt: failed to parse language json: %w", err)
	}

	var langs []string
	for k := range langMap {
		langs = append(langs, k)
	}
	sort.Strings(langs)
	return langs, nil
}

func (m *ModernMT) init() error {
	m.client = translator.NewHTTPClient()

	req, err := http.NewRequest("GET", hostURL, nil)
	if err != nil {
		return fmt.Errorf("modernmt: failed to create init request: %w", err)
	}
	req.Header = translator.HostHeaders(hostURL)

	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("modernmt: failed to fetch main page: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	// 动态获取语言列表
	langs, err := m.fetchLanguages()
	if err != nil {
		return err
	}
	if len(langs) == 0 {
		return fmt.Errorf("modernmt: no supported languages found")
	}
	m.langs = langs

	return nil
}

func (m *ModernMT) Translate(text, from, to string) (*translator.TranslateResult, error) {
	m.once.Do(func() {
		m.initErr = m.init()
	})
	if m.initErr != nil {
		return nil, m.initErr
	}

	if len([]rune(text)) > maxTextLen {
		return nil, fmt.Errorf("modernmt: text exceeds %d character limit", maxTextLen)
	}

	ts := time.Now().UnixMilli()
	verify := sign(text, ts)

	source := from
	if from == "auto" {
		source = ""
	}

	payload := map[string]any{
		"q":         text,
		"source":    source,
		"target":    to,
		"ts":        ts,
		"verify":    verify,
		"hints":     "",
		"multiline": "true",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("modernmt: failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("modernmt: failed to create translate request: %w", err)
	}
	req.Header = translator.APIHeaders(hostURL, "application/json")
	req.Header.Set("X-HTTP-Method-Override", "GET")

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("modernmt: translate request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("modernmt: failed to read response: %w", err)
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("modernmt: failed to parse response: %w", err)
	}

	data, ok := result["data"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("modernmt: unexpected response format: missing data, response: %s", string(respBody))
	}
	translation, ok := data["translation"].(string)
	if !ok {
		return nil, fmt.Errorf("modernmt: unexpected response format: missing translation")
	}

	return &translator.TranslateResult{
		Text:   translation,
		From:   from,
		To:     to,
		Engine: m.Name(),
	}, nil
}

func sign(text string, ts int64) string {
	raw := fmt.Sprintf("%s#%d#%s", webkey, ts, text)
	hash := md5.Sum([]byte(raw))
	return fmt.Sprintf("%x", hash)
}
