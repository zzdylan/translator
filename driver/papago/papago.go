package papago

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	translator "github.com/zzdylan/translator"
)

const (
	host         = "https://papago.naver.com"
	translateURL = "https://papago.naver.com/apis/n2mt/translate"
	detectURL    = "https://papago.naver.com/apis/langs/dect"
	authKey      = "v1.8.10_9e022f68fb"
	maxLen       = 1000
)

// Papago 使用 Naver Papago 翻译引擎实现 Translator 接口。
type Papago struct {
	client   *http.Client
	deviceID string
	langs    []string
	initOnce sync.Once
	initErr  error
}

func New() *Papago {
	return &Papago{}
}

func (t *Papago) Name() string {
	return "papago"
}

func (t *Papago) SupportedLanguages() []string {
	return t.langs
}

func generateUUID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	// 设置 UUID v4 版本和变体位
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]),
		hex.EncodeToString(b[4:6]),
		hex.EncodeToString(b[6:8]),
		hex.EncodeToString(b[8:10]),
		hex.EncodeToString(b[10:16]),
	), nil
}

func (t *Papago) makeAuth(apiURL string, timestamp int64) string {
	msg := fmt.Sprintf("%s\n%s\n%d", t.deviceID, apiURL, timestamp)
	mac := hmac.New(md5.New, []byte(authKey))
	mac.Write([]byte(msg))
	auth := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("PPG %s:%s", t.deviceID, auth)
}

func (t *Papago) doInit() {
	t.client = translator.NewHTTPClient()

	req, err := http.NewRequest("GET", host, nil)
	if err != nil {
		t.initErr = fmt.Errorf("papago: init request error: %w", err)
		return
	}
	req.Header = translator.HostHeaders(host)
	resp, err := t.client.Do(req)
	if err != nil {
		t.initErr = fmt.Errorf("papago: init failed: %w", err)
		return
	}
	defer resp.Body.Close()

	// 读取主页 HTML，用于提取 JS chunk 文件地址
	htmlBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.initErr = fmt.Errorf("papago: failed to read main page: %w", err)
		return
	}
	html := string(htmlBytes)

	// 从 HTML 中提取 JS chunk 文件地址
	chunkRe := regexp.MustCompile(`/home\.(.*?)\.chunk\.js`)
	chunkMatch := chunkRe.FindString(html)
	if chunkMatch == "" {
		t.initErr = fmt.Errorf("papago: failed to extract chunk js url from html")
		return
	}
	chunkURL := "https://papago.naver.com" + chunkMatch

	// 请求 JS chunk 文件
	jsReq, err := http.NewRequest("GET", chunkURL, nil)
	if err != nil {
		t.initErr = fmt.Errorf("papago: failed to create js request: %w", err)
		return
	}
	jsReq.Header = translator.HostHeaders(host)

	jsResp, err := t.client.Do(jsReq)
	if err != nil {
		t.initErr = fmt.Errorf("papago: failed to fetch js chunk: %w", err)
		return
	}
	defer jsResp.Body.Close()

	jsBytes, err := io.ReadAll(jsResp.Body)
	if err != nil {
		t.initErr = fmt.Errorf("papago: failed to read js chunk: %w", err)
		return
	}
	jsContent := string(jsBytes)

	// 从 JS 中提取语言数据 =\{ALL:(.*?)\}
	allRe := regexp.MustCompile(`=\{ALL:(.*?)\}`)
	allMatch := allRe.FindStringSubmatch(jsContent)
	if len(allMatch) < 2 {
		t.initErr = fmt.Errorf("papago: failed to extract language data from js")
		return
	}

	// 提取所有键名（语言代码）
	keyRe := regexp.MustCompile(`,"(.*?)":|,(.*?):`)
	keyMatches := keyRe.FindAllStringSubmatch(allMatch[1], -1)

	var langs []string
	for _, m := range keyMatches {
		key := m[1]
		if key == "" {
			key = m[2]
		}
		// 转换为小写，但修正中文语言代码
		key = strings.ToLower(key)
		if key == "zh-cn" {
			key = "zh-CN"
		} else if key == "zh-tw" {
			key = "zh-TW"
		}
		// 过滤掉 all 和 auto
		if key == "all" || key == "auto" {
			continue
		}
		langs = append(langs, key)
	}

	if len(langs) == 0 {
		t.initErr = fmt.Errorf("papago: no supported languages found")
		return
	}
	sort.Strings(langs)
	t.langs = langs

	t.deviceID, err = generateUUID()
	if err != nil {
		t.initErr = fmt.Errorf("papago: generate device_id error: %w", err)
		return
	}
}

func (t *Papago) setAuthHeaders(req *http.Request, apiURL string, isTranslate bool) {
	ts := time.Now().UnixMilli()
	req.Header.Set("device-type", "pc")
	req.Header.Set("timestamp", fmt.Sprintf("%d", ts))
	req.Header.Set("authorization", t.makeAuth(apiURL, ts))
	if isTranslate {
		req.Header.Set("x-apigw-partnerid", "papago")
	}
}

func (t *Papago) detectLanguage(text string) (string, error) {
	form := url.Values{}
	form.Set("query", text)

	req, err := http.NewRequest("POST", detectURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("papago: detect request error: %w", err)
	}
	req.Header = translator.APIHeaders(host, "")
	t.setAuthHeaders(req, detectURL, false)

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("papago: detect request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("papago: read detect response error: %w", err)
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("papago: parse detect response error: %w", err)
	}

	langCode, ok := result["langCode"].(string)
	if !ok {
		return "", fmt.Errorf("papago: langCode not found in detect response, response: %s", string(respBody))
	}
	return langCode, nil
}

func (t *Papago) Translate(text, from, to string) (*translator.TranslateResult, error) {
	t.initOnce.Do(t.doInit)
	if t.initErr != nil {
		return nil, t.initErr
	}

	if len([]rune(text)) > maxLen {
		return nil, fmt.Errorf("papago: text exceeds %d character limit", maxLen)
	}

	source := from
	if from == "auto" {
		detected, err := t.detectLanguage(text)
		if err != nil {
			return nil, fmt.Errorf("papago: language detection failed: %w", err)
		}
		source = detected
	}

	form := url.Values{}
	form.Set("deviceId", t.deviceID)
	form.Set("text", text)
	form.Set("source", source)
	form.Set("target", to)
	form.Set("locale", "en")
	form.Set("dict", "true")
	form.Set("dictDisplay", "30")
	form.Set("honorific", "false")
	form.Set("instant", "false")
	form.Set("paging", "false")

	req, err := http.NewRequest("POST", translateURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("papago: create request error: %w", err)
	}
	req.Header = translator.APIHeaders(host, "")
	t.setAuthHeaders(req, translateURL, true)

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("papago: translate request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("papago: read response error: %w", err)
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("papago: parse response error: %w", err)
	}

	translatedText, ok := result["translatedText"].(string)
	if !ok {
		return nil, fmt.Errorf("papago: unexpected response format: missing translatedText, response: %s", string(respBody))
	}

	return &translator.TranslateResult{
		Text:   translatedText,
		From:   source,
		To:     to,
		Engine: t.Name(),
	}, nil
}
