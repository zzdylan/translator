package caiyun

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
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
	host         = "https://fanyi.caiyunapp.com"
	translateURL = "https://api.interpreter.caiyunai.com/v1/translator"
	jwtURL       = "https://api.interpreter.caiyunai.com/v1/user/jwt/generate"
	langURL      = "https://fanyi.caiyunapp.com/get_config/xiaoyi_translation_languages.json"
	maxLen       = 5000
)

const (
	normalKey = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789=.+-_/"
	cipherKey = "NOPQRSTUVWXYZABCDEFGHIJKLMnopqrstuvwxyzabcdefghijklm0123456789=.+-_/"
)

// Caiyun 使用彩云小译翻译引擎实现 Translator 接口。
type Caiyun struct {
	client    *http.Client
	browserID string
	jwt       string
	langs     []string
	initOnce  sync.Once
	initErr   error
}

func New() *Caiyun {
	return &Caiyun{}
}

func (t *Caiyun) Name() string {
	return "caiyun"
}

func (t *Caiyun) SupportedLanguages() []string {
	return t.langs
}

// caiyunAPIHeaders 返回彩云 API 请求头（不含 X-Requested-With）。
func caiyunAPIHeaders() http.Header {
	h := http.Header{}
	h.Set("Origin", translator.ExtractOrigin(host))
	h.Set("Referer", host)
	h.Set("Content-Type", "application/json")
	h.Set("User-Agent", translator.UserAgent)
	return h
}

func generateBrowserID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	// 设置 UUID v4 版本和变体位
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	uuid := fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]),
		hex.EncodeToString(b[4:6]),
		hex.EncodeToString(b[6:8]),
		hex.EncodeToString(b[8:10]),
		hex.EncodeToString(b[10:16]),
	)
	// 去除连字符，与 Python 版 str(uuid.uuid4()).replace('-', '') 一致
	return strings.ReplaceAll(uuid, "-", ""), nil
}

func (t *Caiyun) doInit() {
	t.client = translator.NewHTTPClient()

	// 访问主页获取 cookie
	req, err := http.NewRequest("GET", host, nil)
	if err != nil {
		t.initErr = fmt.Errorf("caiyun: init request error: %w", err)
		return
	}
	req.Header = translator.HostHeaders(host)
	resp, err := t.client.Do(req)
	if err != nil {
		t.initErr = fmt.Errorf("caiyun: init failed: %w", err)
		return
	}
	resp.Body.Close()

	// 生成浏览器 ID
	t.browserID, err = generateBrowserID()
	if err != nil {
		t.initErr = fmt.Errorf("caiyun: generate browser_id error: %w", err)
		return
	}

	// 获取 JWT 令牌
	jwtBody, _ := json.Marshal(map[string]string{"browser_id": t.browserID})
	jwtReq, err := http.NewRequest("POST", jwtURL, bytes.NewReader(jwtBody))
	if err != nil {
		t.initErr = fmt.Errorf("caiyun: jwt request error: %w", err)
		return
	}
	jwtReq.Header = caiyunAPIHeaders()
	jwtReq.Header.Set("X-Authorization", "token:qgemv4jr1y38jyq6vhvi")
	jwtReq.Header.Set("app-name", "xiaoyi")
	jwtReq.Header.Set("device-id", t.browserID)
	jwtReq.Header.Set("os-type", "web")
	jwtReq.Header.Set("os-version", "")
	jwtReq.Header.Set("version", "4.6.0")
	jwtReq.Header.Set("Authorization", "bearer")

	jwtResp, err := t.client.Do(jwtReq)
	if err != nil {
		t.initErr = fmt.Errorf("caiyun: jwt request failed: %w", err)
		return
	}
	defer jwtResp.Body.Close()

	jwtRespBody, err := io.ReadAll(jwtResp.Body)
	if err != nil {
		t.initErr = fmt.Errorf("caiyun: read jwt response error: %w", err)
		return
	}

	var jwtResult map[string]any
	if err := json.Unmarshal(jwtRespBody, &jwtResult); err != nil {
		t.initErr = fmt.Errorf("caiyun: parse jwt response error: %w", err)
		return
	}
	jwt, ok := jwtResult["jwt"].(string)
	if !ok {
		t.initErr = fmt.Errorf("caiyun: jwt not found in response, response: %s", string(jwtRespBody))
		return
	}
	t.jwt = jwt

	// 动态获取支持的语言列表
	langs, err := t.fetchLanguages()
	if err != nil {
		t.initErr = fmt.Errorf("caiyun: fetch languages failed: %w", err)
		return
	}
	t.langs = langs
}

// fetchLanguages 从彩云 API 动态获取支持的语言列表。
func (t *Caiyun) fetchLanguages() ([]string, error) {
	req, err := http.NewRequest("GET", langURL, nil)
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

	var data struct {
		SupportedTranslationLanguages []struct {
			Code string `json:"code"`
		} `json:"supported_translation_languages"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}

	langs := make([]string, 0, len(data.SupportedTranslationLanguages))
	for _, item := range data.SupportedTranslationLanguages {
		langs = append(langs, item.Code)
	}
	sort.Strings(langs)
	return langs, nil
}

func decrypt(encrypted string) (string, error) {
	// 将每个字符从密文表位置映射到明文表位置
	var mapped []byte
	for i := 0; i < len(encrypted); i++ {
		idx := strings.IndexByte(cipherKey, encrypted[i])
		if idx >= 0 {
			mapped = append(mapped, normalKey[idx])
		} else {
			mapped = append(mapped, encrypted[i])
		}
	}
	decoded, err := base64.StdEncoding.DecodeString(string(mapped))
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

func (t *Caiyun) Translate(text, from, to string) (*translator.TranslateResult, error) {
	t.initOnce.Do(t.doInit)
	if t.initErr != nil {
		return nil, t.initErr
	}

	if len([]rune(text)) > maxLen {
		return nil, fmt.Errorf("caiyun: text exceeds %d character limit", maxLen)
	}

	transType := from + "2" + to
	if from == "auto" {
		transType = "auto2" + to
	}

	source := strings.Split(text, "\n")

	body := map[string]any{
		"browser_id": t.browserID,
		"source":     source,
		"trans_type": transType,
		"dict":       "true",
		"cached":     "true",
		"replaced":   "true",
		"media":      "text",
		"os_type":    "web",
		"request_id": "web_fanyi",
		"model":      "",
		"style":      "formal",
	}
	if from == "auto" {
		body["detect"] = "true"
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("caiyun: marshal request error: %w", err)
	}

	req, err := http.NewRequest("POST", translateURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("caiyun: create request error: %w", err)
	}
	req.Header = caiyunAPIHeaders()
	req.Header.Set("X-Authorization", "token:qgemv4jr1y38jyq6vhvi")
	req.Header.Set("T-Authorization", t.jwt)
	req.Header.Set("app-name", "xiaoyi")
	req.Header.Set("device-id", t.browserID)
	req.Header.Set("os-type", "web")
	req.Header.Set("os-version", "")
	req.Header.Set("version", "4.6.0")
	req.Header.Set("Authorization", "bearer")

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("caiyun: translate request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("caiyun: read response error: %w", err)
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("caiyun: parse response error: %w", err)
	}

	target, ok := result["target"].([]any)
	if !ok {
		return nil, fmt.Errorf("caiyun: unexpected response format: missing target, response: %s", string(respBody))
	}

	var parts []string
	for _, item := range target {
		s, ok := item.(string)
		if !ok {
			continue
		}
		decrypted, err := decrypt(s)
		if err != nil {
			return nil, fmt.Errorf("caiyun: decrypt error: %w", err)
		}
		parts = append(parts, decrypted)
	}

	detectedFrom := from
	if from == "auto" {
		if df, ok := result["src_tgt"].(string); ok {
			// src_tgt 格式为 "xx2yy"
			if idx := strings.Index(df, "2"); idx > 0 {
				detectedFrom = df[:idx]
			}
		}
	}

	return &translator.TranslateResult{
		Text:   strings.Join(parts, "\n"),
		From:   detectedFrom,
		To:     to,
		Engine: t.Name(),
	}, nil
}
