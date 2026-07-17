package translator

import (
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"time"
)

const (
	UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36"
)

// NewHTTPClient 创建带 cookie jar 和默认超时的 HTTP 客户端。
func NewHTTPClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{
		Jar:     jar,
		Timeout: 30 * time.Second,
	}
}

// ExtractOrigin 从 URL 中提取 scheme+host（不含路径）。
func ExtractOrigin(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	return u.Scheme + "://" + u.Host
}

// HostHeaders 返回访问主页时使用的标准请求头。
func HostHeaders(hostURL string) http.Header {
	h := http.Header{}
	h.Set("Referer", hostURL)
	h.Set("User-Agent", UserAgent)
	return h
}

// APIHeaders 返回 API 请求使用的标准请求头。
func APIHeaders(hostURL string, contentType string) http.Header {
	h := http.Header{}
	h.Set("Origin", ExtractOrigin(hostURL))
	h.Set("Referer", hostURL)
	h.Set("X-Requested-With", "XMLHttpRequest")
	h.Set("User-Agent", UserAgent)
	if contentType != "" {
		h.Set("Content-Type", contentType)
	} else {
		h.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	}
	return h
}
