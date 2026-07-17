package lara

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"

	translator "github.com/zzdylan/translator"
)

const (
	host         = "https://laratranslate.com/translate"
	translateURL = "https://webapi.laratranslate.com/translate/segmented"
	maxLen       = 500
)

// Lara 使用 Lara 翻译引擎实现 Translator 接口。
type Lara struct {
	client   *http.Client
	langs    []string
	initOnce sync.Once
	initErr  error
}

func New() *Lara {
	return &Lara{}
}

func (t *Lara) Name() string {
	return "lara"
}

func (t *Lara) SupportedLanguages() []string {
	return t.langs
}

func (t *Lara) doInit() {
	t.client = translator.NewHTTPClient()
	req, err := http.NewRequest("GET", host, nil)
	if err != nil {
		t.initErr = fmt.Errorf("lara: init request error: %w", err)
		return
	}
	req.Header = translator.HostHeaders(host)
	resp, err := t.client.Do(req)
	if err != nil {
		t.initErr = fmt.Errorf("lara: init failed: %w", err)
		return
	}
	resp.Body.Close()

	// 动态获取支持的语言列表
	langs, err := t.fetchLanguages()
	if err != nil {
		t.initErr = fmt.Errorf("lara: fetch languages failed: %w", err)
		return
	}
	t.langs = langs
}

// fetchLanguages 从接口动态获取支持的语言列表。
func (t *Lara) fetchLanguages() ([]string, error) {
	req, err := http.NewRequest("GET", "https://laratranslate.com/locales/en/common.json", nil)
	if err != nil {
		return nil, err
	}
	req.Header = translator.HostHeaders(host)

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// 响应结构: {"languages": {"en": ..., "zh": ..., ...}}
	var result struct {
		Languages map[string]any `json:"languages"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	if len(result.Languages) == 0 {
		return nil, fmt.Errorf("empty language list")
	}

	langs := make([]string, 0, len(result.Languages))
	for lang := range result.Languages {
		langs = append(langs, lang)
	}
	sort.Strings(langs)
	return langs, nil
}

func (t *Lara) Translate(text, from, to string) (*translator.TranslateResult, error) {
	t.initOnce.Do(t.doInit)
	if t.initErr != nil {
		return nil, t.initErr
	}

	if len([]rune(text)) > maxLen {
		return nil, fmt.Errorf("lara: text exceeds %d character limit", maxLen)
	}

	source := from
	if from == "auto" {
		source = ""
	}

	body := map[string]any{
		"q":            text,
		"source":       source,
		"target":       to,
		"source_hint":  "",
		"style":        "faithful",
		"content_type": "text/plain",
		"adapt_to":     []any{},
		"glossaries":   []any{},
		"instructions": []any{},
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("lara: marshal request error: %w", err)
	}

	req, err := http.NewRequest("POST", translateURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("lara: create request error: %w", err)
	}
	req.Header = translator.APIHeaders(host, "application/json")
	req.Header.Set("X-Lara_Client", "Webapp")

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("lara: translate request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("lara: read response error: %w", err)
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("lara: parse response error: %w", err)
	}

	content, ok := result["content"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("lara: unexpected response format: missing content, response: %s", string(respBody))
	}
	translations, ok := content["translations"].([]any)
	if !ok {
		return nil, fmt.Errorf("lara: unexpected response format: missing translations, response: %s", string(respBody))
	}

	var parts []string
	for _, item := range translations {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if tr, ok := m["translation"].(string); ok {
			parts = append(parts, tr)
		}
	}

	detectedFrom := from
	if from == "auto" {
		detectedFrom = "auto"
	}

	return &translator.TranslateResult{
		Text:   strings.Join(parts, ""),
		From:   detectedFrom,
		To:     to,
		Engine: t.Name(),
	}, nil
}
