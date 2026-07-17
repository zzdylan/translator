package iciba

import (
	"bytes"
	"crypto/aes"
	"crypto/md5"
	"encoding/base64"
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
	hostURL      = "https://www.iciba.com/fy"
	apiURL       = "https://ifanyi.iciba.com/index.php"
	signKey      = "6dVjYLFyzfkFkk"
	encryptKey   = "L4fBtD5fLC9FQw22"
	decryptKey   = "aahc3TfyfCEmER33"
	maxTextLen   = 3000
)

// Iciba 使用金山词霸翻译引擎实现 Translator 接口。
type Iciba struct {
	client   *http.Client
	langs    []string
	initOnce sync.Once
	initErr  error
}

func New() *Iciba {
	return &Iciba{}
}

func (t *Iciba) Name() string {
	return "iciba"
}

func (t *Iciba) SupportedLanguages() []string {
	return t.langs
}

func (t *Iciba) doInit() {
	t.client = translator.NewHTTPClient()

	req, err := http.NewRequest("GET", hostURL, nil)
	if err != nil {
		t.initErr = fmt.Errorf("iciba: init request error: %w", err)
		return
	}
	req.Header = translator.HostHeaders(hostURL)
	resp, err := t.client.Do(req)
	if err != nil {
		t.initErr = fmt.Errorf("iciba: init failed: %w", err)
		return
	}
	resp.Body.Close()

	// 动态获取支持的语言列表
	langs, err := t.fetchLanguages()
	if err != nil {
		t.initErr = fmt.Errorf("iciba: fetch languages failed: %w", err)
		return
	}
	t.langs = langs
}

// fetchLanguages 从接口动态获取支持的语言列表。
func (t *Iciba) fetchLanguages() ([]string, error) {
	req, err := http.NewRequest("GET", "https://ifanyi.iciba.com/index.php?c=trans&m=getLanguage&q=0&type=en&str=", nil)
	if err != nil {
		return nil, err
	}
	// Python 中使用 language_headers（if_api=True, if_json_for_api=True）
	req.Header = translator.APIHeaders(hostURL, "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// 响应结构: {"common": {"zh": "中文", ...}, "A": {"sq": "阿尔巴尼亚语", ...}, ...}
	var result map[string]map[string]string
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	// 提取所有语言代码，去重
	seen := make(map[string]bool)
	for _, group := range result {
		for code := range group {
			seen[code] = true
		}
	}

	langs := make([]string, 0, len(seen))
	for lang := range seen {
		langs = append(langs, lang)
	}
	sort.Strings(langs)
	return langs, nil
}

func makeSign(text string) string {
	raw := fmt.Sprintf("%x", md5.Sum([]byte("6key_web_new_fanyi"+signKey+text)))
	rawSign := raw[:16]
	encrypted := aesECBEncrypt([]byte(rawSign), []byte(encryptKey))
	return base64.StdEncoding.EncodeToString(encrypted)
}

func aesECBEncrypt(data, key []byte) []byte {
	block, _ := aes.NewCipher(key)
	bs := block.BlockSize()
	padding := bs - len(data)%bs
	padtext := bytes.Repeat([]byte{byte(padding)}, padding)
	data = append(data, padtext...)
	out := make([]byte, len(data))
	for i := 0; i < len(data); i += bs {
		block.Encrypt(out[i:i+bs], data[i:i+bs])
	}
	return out
}

func aesECBDecrypt(data, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("创建 AES cipher 失败: %w", err)
	}
	bs := block.BlockSize()
	if len(data) == 0 || len(data)%bs != 0 {
		return nil, fmt.Errorf("密文长度 %d 不是 block size %d 的整数倍", len(data), bs)
	}
	out := make([]byte, len(data))
	for i := 0; i < len(data); i += bs {
		block.Decrypt(out[i:i+bs], data[i:i+bs])
	}
	// PKCS7 unpadding
	padLen := int(out[len(out)-1])
	if padLen < 1 || padLen > bs {
		return nil, fmt.Errorf("无效的 PKCS7 padding 值: %d", padLen)
	}
	return out[:len(out)-padLen], nil
}

func (t *Iciba) Translate(text, from, to string) (*translator.TranslateResult, error) {
	t.initOnce.Do(t.doInit)
	if t.initErr != nil {
		return nil, t.initErr
	}

	if len([]rune(text)) > maxTextLen {
		return nil, fmt.Errorf("iciba: text exceeds %d character limit", maxTextLen)
	}

	sign := makeSign(text)

	apiURLWithParams := fmt.Sprintf("%s?c=trans&m=fyV2&client=6&auth_user=key_web_new_fanyi&sign=%s",
		apiURL, url.QueryEscape(sign))

	reqFrom := from
	reqTo := to
	if from == "auto" {
		reqTo = "auto"
	}

	form := url.Values{}
	form.Set("from", reqFrom)
	form.Set("to", reqTo)
	form.Set("q", text)

	req, err := http.NewRequest("POST", apiURLWithParams, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("iciba: create request error: %w", err)
	}
	req.Header = translator.APIHeaders(hostURL, "")

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("iciba: translate request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("iciba: read response error: %w", err)
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("iciba: parse response error: %w", err)
	}

	content, ok := result["content"].(string)
	if !ok {
		return nil, fmt.Errorf("iciba: unexpected response format: missing content, response: %s", string(body))
	}

	cipherBytes, err := base64.StdEncoding.DecodeString(content)
	if err != nil {
		return nil, fmt.Errorf("iciba: base64 decode error: %w", err)
	}

	decrypted, err := aesECBDecrypt(cipherBytes, []byte(decryptKey))
	if err != nil {
		return nil, fmt.Errorf("iciba: AES 解密失败: %w", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(decrypted, &parsed); err != nil {
		return nil, fmt.Errorf("iciba: parse decrypted content error: %w", err)
	}

	out, ok := parsed["out"].(string)
	if !ok {
		return nil, fmt.Errorf("iciba: unexpected decrypted format: missing out, response: %s", string(decrypted))
	}

	return &translator.TranslateResult{
		Text:   out,
		From:   from,
		To:     to,
		Engine: t.Name(),
	}, nil
}
