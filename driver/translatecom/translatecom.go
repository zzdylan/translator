package translatecom

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
	host         = "https://www.translate.com/machine-translation"
	translateURL = "https://www.translate.com/translator/translate_mt"
	detectURL    = "https://www.translate.com/translator/ajax_lang_auto_detect"
	maxLen       = 15000
)

// TranslateCom 使用 Translate.com 翻译引擎实现 Translator 接口。
type TranslateCom struct {
	client   *http.Client
	langs    []string
	initOnce sync.Once
	initErr  error
}

func New() *TranslateCom {
	return &TranslateCom{}
}

func (t *TranslateCom) Name() string {
	return "translatecom"
}

func (t *TranslateCom) SupportedLanguages() []string {
	return t.langs
}

// fetchLanguages 从 API 获取支持的语言列表。
func (t *TranslateCom) fetchLanguages() ([]string, error) {
	req, err := http.NewRequest("GET", "https://www.translate.com/ajax/language/ht/all", nil)
	if err != nil {
		return nil, fmt.Errorf("translatecom: fetch languages request error: %w", err)
	}
	req.Header = translator.APIHeaders(host, "")

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("translatecom: fetch languages failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("translatecom: read languages response error: %w", err)
	}

	var items []struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("translatecom: parse languages response error: %w", err)
	}

	var langs []string
	for _, item := range items {
		if item.Code != "" {
			langs = append(langs, item.Code)
		}
	}
	sort.Strings(langs)
	return langs, nil
}

func (t *TranslateCom) doInit() {
	t.client = translator.NewHTTPClient()
	req, err := http.NewRequest("GET", host, nil)
	if err != nil {
		t.initErr = fmt.Errorf("translatecom: init request error: %w", err)
		return
	}
	req.Header = translator.HostHeaders(host)
	resp, err := t.client.Do(req)
	if err != nil {
		t.initErr = fmt.Errorf("translatecom: init failed: %w", err)
		return
	}
	resp.Body.Close()

	// 动态获取支持的语言列表
	langs, err := t.fetchLanguages()
	if err != nil {
		t.initErr = err
		return
	}
	t.langs = langs
}

func (t *TranslateCom) Translate(text, from, to string) (*translator.TranslateResult, error) {
	t.initOnce.Do(t.doInit)
	if t.initErr != nil {
		return nil, t.initErr
	}

	if len([]rune(text)) > maxLen {
		return nil, fmt.Errorf("translatecom: text exceeds %d character limit", maxLen)
	}

	if from == "auto" {
		detected, err := t.detectLanguage(text)
		if err != nil {
			return nil, fmt.Errorf("translatecom: language detection failed: %w", err)
		}
		from = detected
	}

	formData := url.Values{}
	formData.Set("text_to_translate", text)
	formData.Set("source_lang", from)
	formData.Set("translated_lang", to)
	formData.Set("use_cache_only", "false")

	req, err := http.NewRequest("POST", translateURL, strings.NewReader(formData.Encode()))
	if err != nil {
		return nil, fmt.Errorf("translatecom: create request error: %w", err)
	}
	req.Header = translator.APIHeaders(host, "")

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("translatecom: translate request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("translatecom: read response error: %w", err)
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("translatecom: parse response error: %w", err)
	}

	translated, ok := result["translated_text"].(string)
	if !ok {
		return nil, fmt.Errorf("translatecom: unexpected response format: missing translated_text, response: %s", string(respBody))
	}

	return &translator.TranslateResult{
		Text:   translated,
		From:   from,
		To:     to,
		Engine: t.Name(),
	}, nil
}

func (t *TranslateCom) detectLanguage(text string) (string, error) {
	formData := url.Values{}
	formData.Set("text_to_translate", text)

	req, err := http.NewRequest("POST", detectURL, strings.NewReader(formData.Encode()))
	if err != nil {
		return "", err
	}
	req.Header = translator.APIHeaders(host, "")

	resp, err := t.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", err
	}

	lang, ok := result["language"].(string)
	if !ok {
		return "", fmt.Errorf("unexpected detection response: missing language, response: %s", string(respBody))
	}
	return lang, nil
}
