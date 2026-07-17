package xunjie

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
	"time"

	translator "github.com/zzdylan/translator"
)

const (
	hostURL       = "https://app.xunjiepdf.com/linefanyi"
	apiURL        = "https://app.xunjiepdf.com/api/v4/quickfanyiweb"
	detectLangURL = "https://app.xunjiepdf.com/api/v4/fanyilanguage"
	langJSURL     = "https://app.xunjiepdf.com/ScriptsMain/hudunjs/jqueryFanyi_new.js"
	maxTextLen    = 500

	// 签名尾部常量
	detectSignTail = "hUuPd20171206LuOnD"
	apiSignTail    = "7w5NPYbXSpNDrP05lC75cUpls0dERvi0asWT"

	// 产品信息（与 Python 实现一致）
	productID      = "146"
	productInfo    = "1245A2A101F776005F2E909C29CC8F7369FAA0BED21AE0A9F9ADBD8D49EE3783"
	productCredits = "ba2ca4a08e8691fd47754ad09927f7f9"
	version        = "4.5.9.3"
)

// Xunjie 使用迅捷翻译引擎实现 Translator 接口。
type Xunjie struct {
	client  *http.Client
	langs   []string
	guid    string
	ver     string // 动态版本号
	once    sync.Once
	initErr error
}

func New() *Xunjie {
	return &Xunjie{}
}

func (x *Xunjie) Name() string {
	return "xunjie"
}

func (x *Xunjie) SupportedLanguages() []string {
	return x.langs
}

func (x *Xunjie) init() error {
	x.client = translator.NewHTTPClient()
	x.guid = generateGUID()
	x.ver = version

	// 访问主页获取 cookies 和动态版本号
	hostReq, err := http.NewRequest("GET", hostURL, nil)
	if err != nil {
		return fmt.Errorf("xunjie: 创建主页请求失败: %w", err)
	}
	hostReq.Header = translator.HostHeaders(hostURL)

	hostResp, err := x.client.Do(hostReq)
	if err != nil {
		return fmt.Errorf("xunjie: 获取主页失败: %w", err)
	}
	defer hostResp.Body.Close()

	hostBytes, err := io.ReadAll(hostResp.Body)
	if err != nil {
		return fmt.Errorf("xunjie: 读取主页失败: %w", err)
	}
	hostHTML := string(hostBytes)

	// 从主页 HTML 中提取版本号
	verRe := regexp.MustCompile(`version=(.*?)"`)
	if verMatch := verRe.FindStringSubmatch(hostHTML); len(verMatch) >= 2 && verMatch[1] != "" {
		x.ver = verMatch[1]
	}

	// 获取语言 JS 文件
	jsReq, err := http.NewRequest("GET", langJSURL+"?version="+x.ver, nil)
	if err != nil {
		return fmt.Errorf("xunjie: 创建 JS 请求失败: %w", err)
	}
	jsReq.Header = translator.HostHeaders(hostURL)

	jsResp, err := x.client.Do(jsReq)
	if err != nil {
		return fmt.Errorf("xunjie: 获取 JS 文件失败: %w", err)
	}
	defer jsResp.Body.Close()

	jsBytes, err := io.ReadAll(jsResp.Body)
	if err != nil {
		return fmt.Errorf("xunjie: 读取 JS 文件失败: %w", err)
	}
	jsContent := string(jsBytes)

	// 从 JS 中提取语言列表: language = {...}
	langRe := regexp.MustCompile(`(?s)language\s*=\s*\{(.*?)\}`)
	langMatch := langRe.FindStringSubmatch(jsContent)
	if len(langMatch) < 2 {
		return fmt.Errorf("xunjie: 无法从 JS 中提取语言列表")
	}

	// 提取所有 key（语言代码）
	keyRe := regexp.MustCompile(`"(\w+)"`)
	keyMatches := keyRe.FindAllStringSubmatch(langMatch[1], -1)
	if len(keyMatches) == 0 {
		// 尝试不带引号的 key
		keyRe = regexp.MustCompile(`(\w+)\s*:`)
		keyMatches = keyRe.FindAllStringSubmatch(langMatch[1], -1)
	}
	if len(keyMatches) == 0 {
		return fmt.Errorf("xunjie: 无法解析语言代码")
	}

	langSet := make(map[string]struct{})
	for _, m := range keyMatches {
		lang := m[1]
		if lang != "" && lang != "language" {
			langSet[lang] = struct{}{}
		}
	}

	var langs []string
	for lang := range langSet {
		langs = append(langs, lang)
	}
	if len(langs) == 0 {
		return fmt.Errorf("xunjie: 未找到支持的语言")
	}
	sort.Strings(langs)
	x.langs = langs

	return nil
}

func (x *Xunjie) Translate(text, from, to string) (*translator.TranslateResult, error) {
	x.once.Do(func() {
		x.initErr = x.init()
	})
	if x.initErr != nil {
		return nil, x.initErr
	}

	if len([]rune(text)) > maxTextLen {
		return nil, fmt.Errorf("xunjie: 文本超过 %d 字符限制", maxTextLen)
	}

	tm := fmt.Sprintf("%d", time.Now().Unix())
	deviceID := md5sum(translator.UserAgent + x.guid)

	apiHeaders := translator.APIHeaders(hostURL, "")
	apiHeaders.Set("X-Version", x.ver)
	apiHeaders.Set("X-Product", productID)
	apiHeaders.Set("X-Credits", productCredits)

	// 处理 auto 语言检测
	fromLang := from
	if fromLang == "auto" {
		// 使用语言检测 API
		detectSign := md5sum(fmt.Sprintf("deviceid=%s&fanyicon=%s&productinfo=%s&timestamp=%s%s",
			deviceID, text, productInfo, tm, detectSignTail))

		detectForm := url.Values{}
		detectForm.Set("fanyicon", text)
		detectForm.Set("timestamp", tm)
		detectForm.Set("productinfo", productInfo)
		detectForm.Set("deviceid", deviceID)
		detectForm.Set("datasign", detectSign)

		detectReq, err := http.NewRequest("POST", detectLangURL, strings.NewReader(detectForm.Encode()))
		if err == nil {
			detectReq.Header = apiHeaders.Clone()
			detectResp, err := x.client.Do(detectReq)
			if err == nil {
				defer detectResp.Body.Close()
				detectBody, err := io.ReadAll(detectResp.Body)
				if err == nil {
					var detectResult map[string]any
					if err := json.Unmarshal(detectBody, &detectResult); err == nil {
						if code, ok := detectResult["getcode"].(string); ok && code != "" {
							fromLang = code
						}
					}
				}
			}
		}
		// 如果检测失败，默认使用 zh
		if fromLang == "auto" {
			fromLang = "zh"
		}
	}

	// 构造翻译签名
	translateSign := md5sum(fmt.Sprintf(
		"deviceid=%s&fanyicon=%s&fanyifrom=%s&fanyito=%s&fontsize=12&height=16&length=4&productinfo=%s&timestamp=%s&width=60%s",
		deviceID, text, fromLang, to, productInfo, tm, apiSignTail))

	// 构造翻译请求
	form := url.Values{}
	form.Set("fanyicon", text)
	form.Set("timestamp", tm)
	form.Set("productinfo", productInfo)
	form.Set("deviceid", deviceID)
	form.Set("datasign", translateSign)
	form.Set("fanyifrom", fromLang)
	form.Set("fanyito", to)
	form.Set("width", "60")
	form.Set("height", "16")
	form.Set("length", "4")
	form.Set("fontsize", "12")

	req, err := http.NewRequest("POST", apiURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("xunjie: 创建翻译请求失败: %w", err)
	}
	req.Header = apiHeaders

	resp, err := x.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("xunjie: 翻译请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("xunjie: 读取响应失败: %w", err)
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("xunjie: 解析响应失败: %w (body: %s)", err, string(body))
	}

	translatedText, ok := result["txtcontent"].(string)
	if !ok {
		return nil, fmt.Errorf("xunjie: 响应中缺少 txtcontent 字段, response: %s", string(body))
	}

	// 清理多余空格
	translatedText = strings.ReplaceAll(translatedText, "\n ", "\n")

	return &translator.TranslateResult{
		Text:   translatedText,
		From:   from,
		To:     to,
		Engine: x.Name(),
	}, nil
}

func md5sum(s string) string {
	hash := md5.Sum([]byte(s))
	return hex.EncodeToString(hash[:])
}

func generateGUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
