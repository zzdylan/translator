package qqtransmart

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	translator "github.com/zzdylan/translator"
)

const (
	hostURL    = "https://transmart.qq.com"
	apiURL     = "https://transmart.qq.com/api/imt"
	maxTextLen = 5000
)

// QQTranSmart 使用腾讯交互翻译引擎实现 Translator 接口。
type QQTranSmart struct {
	client  *http.Client
	langs   []string
	uuid    string
	once    sync.Once
	initErr error
}

func New() *QQTranSmart {
	return &QQTranSmart{}
}

func (q *QQTranSmart) Name() string {
	return "qqtransmart"
}

func (q *QQTranSmart) SupportedLanguages() []string {
	return q.langs
}

func (q *QQTranSmart) init() error {
	q.client = translator.NewHTTPClient()
	q.uuid = generateUUID()

	// 获取主页 HTML
	req, err := http.NewRequest("GET", hostURL, nil)
	if err != nil {
		return fmt.Errorf("qqtransmart: 创建初始化请求失败: %w", err)
	}
	req.Header = translator.HostHeaders(hostURL)

	resp, err := q.client.Do(req)
	if err != nil {
		return fmt.Errorf("qqtransmart: 获取主页失败: %w", err)
	}
	defer resp.Body.Close()

	htmlBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("qqtransmart: 读取主页失败: %w", err)
	}
	html := string(htmlBytes)

	// 从 HTML 中提取 vendor JS URL
	vendorRe := regexp.MustCompile(`/assets/vendor\.(.*?)\.js`)
	vendorMatch := vendorRe.FindString(html)
	if vendorMatch == "" {
		return fmt.Errorf("qqtransmart: 无法从主页提取 vendor JS URL")
	}
	vendorURL := hostURL + vendorMatch

	// 获取 vendor JS 文件
	jsReq, err := http.NewRequest("GET", vendorURL, nil)
	if err != nil {
		return fmt.Errorf("qqtransmart: 创建 JS 请求失败: %w", err)
	}
	jsReq.Header = translator.HostHeaders(hostURL)

	jsResp, err := q.client.Do(jsReq)
	if err != nil {
		return fmt.Errorf("qqtransmart: 获取 JS 文件失败: %w", err)
	}
	defer jsResp.Body.Close()

	jsBytes, err := io.ReadAll(jsResp.Body)
	if err != nil {
		return fmt.Errorf("qqtransmart: 读取 JS 文件失败: %w", err)
	}
	jsContent := string(jsBytes)

	// 从 JS 中提取语言列表 lngs:[...]
	lngsRe := regexp.MustCompile(`lngs:\[(.*?)\]`)
	lngsMatches := lngsRe.FindAllStringSubmatch(jsContent, -1)
	if len(lngsMatches) == 0 {
		return fmt.Errorf("qqtransmart: 无法从 JS 中提取语言列表")
	}

	// 从匹配结果中提取所有语言代码（同时支持双引号和单引号）
	langSet := make(map[string]struct{})
	codeRe := regexp.MustCompile(`["']([a-zA-Z][a-zA-Z0-9-]*)["']`)
	for _, m := range lngsMatches {
		codes := codeRe.FindAllStringSubmatch(m[1], -1)
		for _, c := range codes {
			if c[1] != "" {
				langSet[c[1]] = struct{}{}
			}
		}
	}

	var langs []string
	for lang := range langSet {
		langs = append(langs, lang)
	}
	if len(langs) == 0 {
		return fmt.Errorf("qqtransmart: 未找到支持的语言")
	}
	sort.Strings(langs)
	q.langs = langs

	return nil
}

func (q *QQTranSmart) Translate(text, from, to string) (*translator.TranslateResult, error) {
	q.once.Do(func() {
		q.initErr = q.init()
	})
	if q.initErr != nil {
		return nil, q.initErr
	}

	if len([]rune(text)) > maxTextLen {
		return nil, fmt.Errorf("qqtransmart: 文本超过 %d 字符限制", maxTextLen)
	}

	// 处理 auto 语言：QQTranSmart 不支持 auto，默认使用 zh
	fromLang := from
	if fromLang == "auto" {
		fromLang = "zh"
	}

	// 每次翻译重新生成 clientKey（与 Python 一致，uuid 不变但时间戳更新）
	clientKey := fmt.Sprintf("browser-firefox-110.0.0-Windows 10-%s-%d", q.uuid, time.Now().UnixMilli())

	headers := translator.APIHeaders(hostURL, "application/json")
	headers.Set("Cookie", fmt.Sprintf("client_key=%s", clientKey))

	// 第一步：文本分析（拆分句子）
	splitPayload := map[string]any{
		"header": map[string]any{
			"fn":         "text_analysis",
			"client_key": clientKey,
		},
		"type":      "plain",
		"text":      text,
		"normalize": map[string]any{"merge_broken_line": "false"},
	}

	splitData, err := q.postJSON(apiURL, splitPayload, headers)
	if err != nil {
		return nil, fmt.Errorf("qqtransmart: 文本分析请求失败: %w", err)
	}

	// 解析句子列表
	textList := splitSentence(splitData, text)

	// 第二步：执行翻译
	apiPayload := map[string]any{
		"header": map[string]any{
			"fn":         "auto_translation",
			"client_key": clientKey,
		},
		"type":           "plain",
		"model_category": "normal",
		"source": map[string]any{
			"lang":      fromLang,
			"text_list": append(append([]string{""}, textList...), ""),
		},
		"target": map[string]any{
			"lang": to,
		},
	}

	data, err := q.postJSON(apiURL, apiPayload, headers)
	if err != nil {
		return nil, fmt.Errorf("qqtransmart: 翻译请求失败: %w", err)
	}

	// 解析翻译结果
	autoTranslation, ok := data["auto_translation"].([]any)
	if !ok {
		return nil, fmt.Errorf("qqtransmart: 响应中缺少 auto_translation 字段, response: %v", data)
	}

	var sb strings.Builder
	for _, item := range autoTranslation {
		if s, ok := item.(string); ok {
			sb.WriteString(s)
		}
	}

	return &translator.TranslateResult{
		Text:   sb.String(),
		From:   from,
		To:     to,
		Engine: q.Name(),
	}, nil
}

// postJSON 发送 JSON POST 请求并返回解析后的响应。
func (q *QQTranSmart) postJSON(url string, payload any, headers http.Header) (map[string]any, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header = headers

	resp, err := q.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w (body: %s)", err, string(body))
	}

	return result, nil
}

// splitSentence 根据文本分析结果拆分原文为句子列表。
// 与 Python 实现一致：将 [start, start+len] 对展平为索引列表，
// 然后对所有相邻索引做切片，保留句间文本。
func splitSentence(data map[string]any, text string) []string {
	// 优先使用 API 返回的 text 字段（可能经过 normalize）
	srcText := text
	if t, ok := data["text"].(string); ok && t != "" {
		srcText = t
	}

	sentenceList, ok := data["sentence_list"].([]any)
	if !ok || len(sentenceList) == 0 {
		return []string{srcText}
	}

	// 构建索引对列表: [s1, e1, s2, e2, ...]
	var indexList []int
	for _, item := range sentenceList {
		s, ok := item.(map[string]any)
		if !ok {
			continue
		}
		start, ok1 := s["start"].(float64)
		length, ok2 := s["len"].(float64)
		if !ok1 || !ok2 {
			continue
		}
		st := int(start)
		ln := int(length)
		indexList = append(indexList, st, st+ln)
	}

	if len(indexList) < 2 {
		return []string{srcText}
	}

	// 对所有相邻索引做切片（与 Python 一致，保留空字符串）
	runes := []rune(srcText)
	var result []string
	for i := 0; i < len(indexList)-1; i++ {
		from := indexList[i]
		to := indexList[i+1]
		if to > len(runes) {
			continue
		}
		result = append(result, string(runes[from:to]))
	}

	if len(result) == 0 {
		return []string{srcText}
	}
	return result
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
