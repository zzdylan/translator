package sogou

import (
	"crypto/md5"
	"crypto/rand"
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

	translator "github.com/zzdylan/translator"
)

const (
	hostURL    = "https://fanyi.sogou.com/text"
	apiURL     = "https://fanyi.sogou.com/api/transpc/text/result"
	secretKey  = "109984457"
	maxTextLen = 5000
)

// Sogou 使用搜狗翻译引擎实现 Translator 接口。
type Sogou struct {
	client  *http.Client
	uuid    string
	langs   []string
	once    sync.Once
	initErr error
}

func New() *Sogou {
	return &Sogou{}
}

func (s *Sogou) Name() string {
	return "sogou"
}

func (s *Sogou) SupportedLanguages() []string {
	return s.langs
}

func (s *Sogou) init() error {
	s.client = translator.NewHTTPClient()
	s.uuid = generateUUID()

	req, err := http.NewRequest("GET", hostURL, nil)
	if err != nil {
		return fmt.Errorf("sogou: failed to create init request: %w", err)
	}
	req.Header = translator.HostHeaders(hostURL)

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("sogou: failed to fetch main page: %w", err)
	}
	defer resp.Body.Close()

	// 读取主页 HTML，用于提取 JS 文件地址
	htmlBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("sogou: failed to read main page: %w", err)
	}
	html := string(htmlBytes)

	// 从 HTML 中提取 JS 文件地址
	jsURL := "https://search.sogoucdn.com/translate/pc/static/js/app.7016e0df.js"
	jsRe := regexp.MustCompile(`//search\.sogoucdn\.com/translate/pc/static/js/vendors\.(.*?)\.js`)
	if m := jsRe.FindString(html); m != "" {
		jsURL = "https:" + m
	}

	// 请求 JS 文件
	jsReq, err := http.NewRequest("GET", jsURL, nil)
	if err != nil {
		return fmt.Errorf("sogou: failed to create js request: %w", err)
	}
	jsReq.Header = translator.HostHeaders(hostURL)

	jsResp, err := s.client.Do(jsReq)
	if err != nil {
		return fmt.Errorf("sogou: failed to fetch js file: %w", err)
	}
	defer jsResp.Body.Close()

	jsBytes, err := io.ReadAll(jsResp.Body)
	if err != nil {
		return fmt.Errorf("sogou: failed to read js file: %w", err)
	}
	jsContent := string(jsBytes)

	// 从 JS 中提取语言列表 "ALL":[...]
	allRe := regexp.MustCompile(`"ALL":\[(.*?)\]`)
	allMatch := allRe.FindStringSubmatch(jsContent)
	if len(allMatch) < 2 {
		return fmt.Errorf("sogou: failed to extract language list from js")
	}

	// 替换 !0 -> 1, !1 -> 0，使其成为合法 JSON
	langJSON := allMatch[1]
	langJSON = strings.ReplaceAll(langJSON, "!0", "1")
	langJSON = strings.ReplaceAll(langJSON, "!1", "0")
	langJSON = "[" + langJSON + "]"

	// 解析语言列表
	var items []struct {
		Lang string `json:"lang"`
		Play int    `json:"play"`
	}
	if err := json.Unmarshal([]byte(langJSON), &items); err != nil {
		return fmt.Errorf("sogou: failed to parse language list: %w", err)
	}

	// 筛选 play == 1 的语言
	var langs []string
	for _, item := range items {
		if item.Play == 1 {
			langs = append(langs, item.Lang)
		}
	}
	if len(langs) == 0 {
		return fmt.Errorf("sogou: no supported languages found")
	}
	sort.Strings(langs)
	s.langs = langs

	return nil
}

func (s *Sogou) Translate(text, from, to string) (*translator.TranslateResult, error) {
	s.once.Do(func() {
		s.initErr = s.init()
	})
	if s.initErr != nil {
		return nil, s.initErr
	}

	if len([]rune(text)) > maxTextLen {
		return nil, fmt.Errorf("sogou: text exceeds %d character limit", maxTextLen)
	}

	sign := sign(from, to, text)

	form := url.Values{}
	form.Set("from", from)
	form.Set("to", to)
	form.Set("text", text)
	form.Set("uuid", s.uuid)
	form.Set("s", sign)
	form.Set("client", "pc")
	form.Set("fr", "browser_pc")
	form.Set("needQc", "1")

	req, err := http.NewRequest("POST", apiURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("sogou: failed to create translate request: %w", err)
	}
	req.Header = translator.APIHeaders(hostURL, "")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sogou: translate request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("sogou: failed to read response: %w", err)
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("sogou: failed to parse response: %w", err)
	}

	data, ok := result["data"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("sogou: unexpected response format: missing data, response: %s", string(body))
	}
	translateData, ok := data["translate"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("sogou: unexpected response format: missing translate, response: %s", string(body))
	}
	dit, ok := translateData["dit"].(string)
	if !ok {
		return nil, fmt.Errorf("sogou: unexpected response format: missing dit, response: %s", string(body))
	}

	return &translator.TranslateResult{
		Text:   dit,
		From:   from,
		To:     to,
		Engine: s.Name(),
	}, nil
}

func sign(from, to, text string) string {
	raw := from + to + text + secretKey
	hash := md5.Sum([]byte(raw))
	return fmt.Sprintf("%x", hash)
}

func generateUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]),
		hex.EncodeToString(b[4:6]),
		hex.EncodeToString(b[6:8]),
		hex.EncodeToString(b[8:10]),
		hex.EncodeToString(b[10:16]),
	)
}
