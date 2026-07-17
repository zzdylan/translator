package iflyrec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	translator "github.com/zzdylan/translator"
)

const (
	host       = "https://fanyi.iflyrec.com"
	translateURL = "https://fanyi.iflyrec.com/TranslationService/v1/textAutoTranslation"
	detectURL    = "https://fanyi.iflyrec.com/TranslationService/v1/languageDetection"
	maxLen     = 2000
)

var langMap = map[string]int{
	"zh": 1, "en": 2, "ja": 3, "ko": 4, "ru": 5, "fr": 6,
	"es": 7, "vi": 8, "yue": 9, "ar": 12, "de": 13, "it": 14,
}

var numToLang map[int]string

func init() {
	numToLang = make(map[int]string, len(langMap))
	for k, v := range langMap {
		numToLang[v] = k
	}
}

// IFlyRec 使用讯飞听见翻译引擎实现 Translator 接口。
type IFlyRec struct {
	client   *http.Client
	initOnce sync.Once
	initErr  error
}

func New() *IFlyRec {
	return &IFlyRec{}
}

func (t *IFlyRec) Name() string {
	return "iflyrec"
}

func (t *IFlyRec) SupportedLanguages() []string {
	langs := make([]string, 0, len(langMap))
	for k := range langMap {
		langs = append(langs, k)
	}
	return langs
}

func (t *IFlyRec) doInit() {
	t.client = translator.NewHTTPClient()
	req, err := http.NewRequest("GET", host, nil)
	if err != nil {
		t.initErr = fmt.Errorf("iflyrec: init request error: %w", err)
		return
	}
	req.Header = translator.HostHeaders(host)
	resp, err := t.client.Do(req)
	if err != nil {
		t.initErr = fmt.Errorf("iflyrec: init failed: %w", err)
		return
	}
	resp.Body.Close()
}

func (t *IFlyRec) Translate(text, from, to string) (*translator.TranslateResult, error) {
	t.initOnce.Do(t.doInit)
	if t.initErr != nil {
		return nil, t.initErr
	}

	if len([]rune(text)) > maxLen {
		return nil, fmt.Errorf("iflyrec: text exceeds %d character limit", maxLen)
	}

	if from == "auto" {
		detected, err := t.detectLanguage(text)
		if err != nil {
			return nil, fmt.Errorf("iflyrec: language detection failed: %w", err)
		}
		from = detected
	}

	fromID, ok := langMap[from]
	if !ok {
		return nil, fmt.Errorf("iflyrec: unsupported source language: %s", from)
	}
	toID, ok := langMap[to]
	if !ok {
		return nil, fmt.Errorf("iflyrec: unsupported target language: %s", to)
	}

	var contents []map[string]any
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		contents = append(contents, map[string]any{
			"text":           line,
			"frontBlankLine": 0,
		})
	}

	body := map[string]any{
		"from":            fromID,
		"to":              toID,
		"openTerminology": "false",
		"contents":        contents,
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("iflyrec: marshal request error: %w", err)
	}

	ts := fmt.Sprintf("%d", time.Now().UnixMilli())
	apiURL := translateURL + "?t=" + ts

	req, err := http.NewRequest("POST", apiURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("iflyrec: create request error: %w", err)
	}
	req.Header = translator.APIHeaders(host, "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("iflyrec: translate request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("iflyrec: read response error: %w", err)
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("iflyrec: parse response error: %w", err)
	}

	biz, ok := result["biz"].([]any)
	if !ok {
		return nil, fmt.Errorf("iflyrec: unexpected response format: missing biz, response: %s", string(respBody))
	}

	var parts []string
	for _, item := range biz {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if tr, ok := m["translateResult"].(string); ok {
			parts = append(parts, tr)
		}
	}

	return &translator.TranslateResult{
		Text:   strings.Join(parts, "\n"),
		From:   from,
		To:     to,
		Engine: t.Name(),
	}, nil
}

func (t *IFlyRec) detectLanguage(text string) (string, error) {
	body := map[string]string{"originalText": text}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	ts := fmt.Sprintf("%d", time.Now().UnixMilli())
	apiURL := detectURL + "?t=" + ts

	req, err := http.NewRequest("POST", apiURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}
	req.Header = translator.APIHeaders(host, "application/json")

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

	biz, ok := result["biz"].([]any)
	if !ok || len(biz) == 0 {
		return "", fmt.Errorf("unexpected detection response: missing biz")
	}
	item, ok := biz[0].(map[string]any)
	if !ok {
		return "", fmt.Errorf("unexpected detection response: invalid biz item")
	}

	langID, ok := item["detectionLanguage"].(float64)
	if !ok {
		return "", fmt.Errorf("unexpected detection response: missing detectionLanguage")
	}

	lang, ok := numToLang[int(langID)]
	if !ok {
		return "", fmt.Errorf("unknown detected language ID: %d", int(langID))
	}
	return lang, nil
}
