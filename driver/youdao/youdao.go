package youdao

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	translator "github.com/zzdylan/translator"
)

const (
	host         = "https://fanyi.youdao.com"
	translateURL = "https://fanyi.youdao.com/translate_o?smartresult=dict&smartresult=rule"
	signKey      = "Ygy_4c=r#e#4EX^NUGUc5"
	maxLen       = 5000
)

// Youdao 使用有道翻译引擎实现 Translator 接口。
// 注意：有道签名机制已变更，当前驱动需要修复。
type Youdao struct {
	client   *http.Client
	langs    []string
	initOnce sync.Once
	initErr  error
}

func New() *Youdao {
	return &Youdao{}
}

func (t *Youdao) Name() string {
	return "youdao"
}

func (t *Youdao) SupportedLanguages() []string {
	return t.langs
}

func md5sum(s string) string {
	h := md5.Sum([]byte(s))
	return fmt.Sprintf("%x", h)
}

func (t *Youdao) doInit() {
	t.client = translator.NewHTTPClient()

	req, err := http.NewRequest("GET", host, nil)
	if err != nil {
		t.initErr = fmt.Errorf("youdao: init request error: %w", err)
		return
	}
	req.Header = translator.HostHeaders(host)
	resp, err := t.client.Do(req)
	if err != nil {
		t.initErr = fmt.Errorf("youdao: init failed: %w", err)
		return
	}
	resp.Body.Close()

	// 动态获取支持的语言列表
	langs, err := t.fetchLanguages()
	if err != nil {
		t.initErr = fmt.Errorf("youdao: fetch languages failed: %w", err)
		return
	}
	t.langs = langs
}

// fetchLanguages 从接口动态获取支持的语言列表。
func (t *Youdao) fetchLanguages() ([]string, error) {
	req, err := http.NewRequest("GET", "https://api-overmind.youdao.com/openapi/get/luna/dict/luna-front/prod/langType", nil)
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

	// 响应结构: {"data": {"value": {"textTranslate": {"specify": [{"code": "xx"}, ...]}}}}
	var result struct {
		Data struct {
			Value struct {
				TextTranslate struct {
					Specify []struct {
						Code string `json:"code"`
					} `json:"specify"`
				} `json:"textTranslate"`
			} `json:"value"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	items := result.Data.Value.TextTranslate.Specify
	if len(items) == 0 {
		return nil, fmt.Errorf("empty language list")
	}

	langs := make([]string, 0, len(items))
	for _, item := range items {
		if item.Code != "" {
			langs = append(langs, item.Code)
		}
	}
	sort.Strings(langs)
	return langs, nil
}

func (t *Youdao) Translate(text, from, to string) (*translator.TranslateResult, error) {
	t.initOnce.Do(t.doInit)
	if t.initErr != nil {
		return nil, t.initErr
	}

	if len([]rune(text)) > maxLen {
		return nil, fmt.Errorf("youdao: text exceeds %d character limit", maxLen)
	}

	apiFrom := from

	ts := fmt.Sprintf("%d", time.Now().UnixMilli())
	salt := ts + fmt.Sprintf("%d", rand.Intn(10))
	sign := md5sum("fanyideskweb" + text + salt + signKey)
	bv := md5sum(translator.UserAgent[8:])

	form := url.Values{}
	form.Set("i", text)
	form.Set("from", apiFrom)
	form.Set("to", to)
	form.Set("lts", ts)
	form.Set("salt", salt)
	form.Set("sign", sign)
	form.Set("bv", bv)
	form.Set("smartresult", "dict")
	form.Set("client", "fanyideskweb")
	form.Set("doctype", "json")
	form.Set("version", "2.1")
	form.Set("keyfrom", "fanyi.web")
	form.Set("action", "FY_BY_REALTlME")

	req, err := http.NewRequest("POST", translateURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("youdao: create request error: %w", err)
	}
	req.Header = translator.APIHeaders(host, "")

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("youdao: translate request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("youdao: read response error: %w", err)
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("youdao: parse response error: %w", err)
	}

	translateResult, ok := result["translateResult"].([]any)
	if !ok {
		return nil, fmt.Errorf("youdao: unexpected response format: missing translateResult, response: %s", string(respBody))
	}

	var lines []string
	for _, outer := range translateResult {
		outerArr, ok := outer.([]any)
		if !ok {
			continue
		}
		var parts []string
		for _, inner := range outerArr {
			m, ok := inner.(map[string]any)
			if !ok {
				continue
			}
			if tgt, ok := m["tgt"].(string); ok {
				parts = append(parts, tgt)
			}
		}
		lines = append(lines, strings.Join(parts, " "))
	}

	detectedFrom := from
	if from == "auto" {
		if tp, ok := result["type"].(string); ok {
			// type 格式为 "xx2yy"
			if idx := strings.Index(tp, "2"); idx > 0 {
				detectedFrom = tp[:idx]
			}
		}
	}

	return &translator.TranslateResult{
		Text:   strings.Join(lines, "\n"),
		From:   detectedFrom,
		To:     to,
		Engine: t.Name(),
	}, nil
}
