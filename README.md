# Translator SDK

Go 翻译 SDK，移植自 Python 开源逆向翻译库 [UlionTse/translators](https://github.com/UlionTse/translators)，提供统一接口调用多个翻译引擎。

## 安装

```bash
go get github.com/zzdylan/translator
```

## 统一接口

```go
type Translator interface {
    Translate(text, from, to string) (*TranslateResult, error)
    SupportedLanguages() []string
    Name() string
}

type TranslateResult struct {
    Text   string // 翻译结果
    From   string // 源语言
    To     string // 目标语言
    Engine string // 引擎名称
}
```

## 使用示例

```go
package main

import (
    "fmt"
    "github.com/zzdylan/translator/driver/caiyun"
)

func main() {
    t := caiyun.New()
    result, err := t.Translate("你好，世界", "auto", "en")
    if err != nil {
        panic(err)
    }
    fmt.Println(result.Text) // Hello, world
}
```

## 引擎实现状态

### 已实现（16 个）

纯 Go 实现，无外部依赖，协议与 Python 原版一致。

| 引擎 | 包路径 | 字符限制 | 签名方式 | 备注 |
|------|--------|---------|---------|------|
| 讯飞听见 | `driver/iflyrec` | 2000 | 无 | 支持语言检测 |
| 彩云小译 | `driver/caiyun` | 5000 | JWT + ROT13 变体解密 | 响应需解密 |
| 搜狗翻译 | `driver/sogou` | 5000 | MD5 | |
| 金山词霸 | `driver/iciba` | 3000 | MD5 + AES-128-ECB | 请求签名加密，响应 AES 解密 |
| 阿里翻译 | `driver/alibaba` | 5000 | CSRF Token | V2 协议，multipart/form-data |
| 有道翻译 | `driver/youdao` | 5000 | MD5 | |
| Papago | `driver/papago` | 1000 | HMAC-MD5 | 支持语言检测 |
| ModernMT | `driver/modernmt` | 5000 | MD5 | |
| Lara | `driver/lara` | 500 | 无 | |
| Lingvanex | `driver/lingvanex` | 1000 | Bearer Token（从页面提取） | V2 协议 |
| Translate.com | `driver/translatecom` | 15000 | 无 | 支持语言检测，字符限制最大 |
| Yandex | `driver/yandex` | 10000 | 无 | V2 协议，支持语言检测 |
| Bing | `driver/bing` | 1000 | AbusePreventionHelper token | 使用 cn.bing.com；token 当前从 HTML 正则提取，若改为 JS 动态生成则需嵌入 JS 引擎（如 goja） |
| iTranslate | `driver/itranslate` | 1000 | API-KEY（从 main.js 提取） | 语言代码映射：zh→zh-CN, en→en-US |
| 腾讯交互翻译 | `driver/qqtransmart` | 5000 | 无 | 两步翻译：text_analysis→auto_translation |
| 迅捷翻译 | `driver/xunjie` | 500 | MD5 | 支持语言检测；动态版本号提取 |

### 暂不可用（1 个）

| 引擎 | 包路径 | 说明 |
|------|--------|------|
| Reverso | `driver/reverso` | Cloudflare Managed Challenge 保护，Python 用 cloudscraper 绕过，Go 暂无验证过的可行方案 |

## 运行示例

```bash
cd example
go run main.go
```

并发调用全部 16 个已实现引擎翻译 "你好，世界" 为英文。

## 翻译质量对比

使用 5 个场景测试各引擎翻译质量（`go test -run TestTranslationQuality`）。

### 综合对比

| 引擎 | 字符限制 | 语言数 | 平均响应 | 通过率 | 中→英质量 | 英→中质量 | 稳定性 | 推荐度 |
|------|---------|--------|---------|-------|----------|----------|--------|--------|
| bing (必应) | 1000 | 179 | ~2.0s | 5/5 | ★★★★★ | ★★★★★ | ★★★★ | **首选** |
| iflyrec (讯飞) | 2000 | 硬编码 | ~1.1s | 5/5 | ★★★★★ | ★★★★★ | ★★★★★ | **首选** |
| sogou (搜狗) | 5000 | 20 | ~0.6s | 5/5 | ★★★★ | ★★★★★ | ★★★★★ | **推荐** |
| caiyun (彩云) | 5000 | 20 | ~0.9s | 5/5 | ★★★★ | ★★★★★ | ★★★★★ | **推荐** |
| qqtransmart (腾讯) | 5000 | 135 | ~1.0s | 5/5 | ★★★★ | ★★★★★ | ★★★★★ | **推荐** |
| lara | 500 | 211 | ~1.4s | 5/5 | ★★★★ | ★★★★★ | ★★★★ | 推荐 |
| modernmt | 5000 | 200 | ~1.5s | 5/5 | ★★★★ | ★★★★★ | ★★★★ | 推荐 |
| yandex | 10000 | 34 | ~1.3s | 5/5 | ★★★★ | — | ★★★★ | 推荐 |
| xunjie (迅捷) | 500 | 68 | ~1.4s | 5/5 | ★★★☆ | ★★★★★ | ★★★★ | 可用 |
| lingvanex | 1000 | 116 | ~1.3s | 4/5 | ★★★★ | ★★★ | ★★★ | 可用 |
| itranslate | 1000 | 101 | ~2.1s | 5/5 | ★★★ | ★★★★★ | ★★★★ | 可用 |
| alibaba (阿里) | 5000 | 221 | ~0.8s | 4/5 | ★★★★ | ★★★★★ | ★★★ | 可用 |
| translatecom | 15000 | 38 | — | 1/5 | — | — | ★ | 不稳定 |
| iciba (金山) | 3000 | 187 | ~0.7s | 4/5 | ★★★★ | ★★★★★ | ★★★ | 可用（API 偶发解密异常） |
| youdao (有道) | 5000 | — | — | — | — | — | ★ | 签名失效 |
| reverso | 2000 | — | — | — | — | — | — | Cloudflare 阻断 |

**选型建议：**

- **质量优先**：bing、iflyrec — 译文最自然流畅，语法准确，用词地道
- **大文本**：sogou、qqtransmart、caiyun（5000 字符）、yandex（10000 字符）
- **语言覆盖**：alibaba（221）、lara（211）、modernmt（200）、bing（179）
- **响应速度**：sogou（~0.6s）、caiyun（~0.9s）、qqtransmart（~1.0s）
- **综合推荐**：**bing** 质量最佳 + 语言多，**qqtransmart** 质量好 + 字符大 + 速度快

### 场景 1：日常对话

> 今天天气真好，我打算去公园散步。下午三点我们在咖啡厅见面，你觉得怎么样？

| 引擎 | 译文 |
|------|------|
| iflyrec | It's a nice day today. I'm going to take a walk in the park. Let's meet at the coffee shop at 3 pm. What do you think? |
| caiyun | It's a beautiful day today. I'm going to take a walk in the park. What do you say we meet at the cafe at 3 p.m. ? |
| sogou | It's a beautiful day today. I'm going to take a walk in the park. What do you say we meet in the coffee shop at three o'clock in the afternoon? |
| bing | The weather is really nice today, I plan to go for a walk in the park. We can meet at the cafe at three in the afternoon, what do you think? |
| itranslate | It was nice today, and I'm going to go for a walk in the park. We met at the cafe at 3 p.m. What do you think? |
| qqtransmart | It's a nice day today. I'm going for a walk in the park. What do you say we meet at the cafe at 3:00 p.m.? |
| xunjie | It's such a nice day today. I'm going to go for a walk in the park. We will meet at the cafe at three o'clock in the afternoon. What do you think? |

### 场景 2：技术文档

> Go语言是一种静态类型、编译型的编程语言。它由Google开发，具有高效的并发处理能力。goroutine是Go语言中轻量级的线程实现。

| 引擎 | 译文 |
|------|------|
| iflyrec | Go is a statically typed, compiled programming language. Developed by Google, it has efficient concurrent processing capabilities. The goroutine is a lightweight threading implementation in the Go language. |
| caiyun | Go is a statically typed, compiled programming language. It was developed by Google and is highly efficient in concurrency. Goroutine is a lightweight threading implementation of the Go language. |
| sogou | Go language is a static and compiled programming language. Developed by Google, it has efficient concurrent processing ability. Goroutine is a lightweight thread implementation in Go language. |
| bing | The Go language is a statically typed, compiled programming language. It was developed by Google and has efficient concurrency handling capabilities. Goroutine is a lightweight thread implementation in the Go language. |
| itranslate | Go is a static, compiled programming language. It is developed by Google and has efficient concurrent processing power. goroutine is a lightweight thread implementation in Go. |
| qqtransmart | Go is a statically typed, compiled programming language. Developed by Google, it has efficient concurrency. Goroutine is a lightweight thread implementation in Go. |
| xunjie | Go language is a static type and compiled programming language. It is developed by Google and has efficient syncurrent processing capabilities. Goroutine is a lightweight thread implementation in Go language. |

### 场景 3：数字与专有名词混合

> 2024年巴黎奥运会共设32个大项、329个小项。中国代表团派出了405名运动员参赛。

| 引擎 | 译文 |
|------|------|
| iflyrec | There are 32 major events and 329 minor events in the 2024 Paris Olympic Games. The Chinese delegation sent 405 athletes to participate in the competition. |
| caiyun | Paris 2024 Olympic Games a total of 32 major events, 329 events. The Chinese delegation sent 405 athletes to the games. |
| sogou | There are 32 major events and 329 minor events in the 2024 Paris Olympic Games. The China delegation sent 405 athletes to participate in the competition. |
| bing | The 2024 Paris Olympics will feature 32 major events and 329 sub-events. The Chinese delegation sent 405 athletes to compete. |
| itranslate | The 2024 Paris Olympics will have 32 major and 329 small items. The Chinese delegation sent 405 athletes to participate. |
| qqtransmart | The 2024 Paris Olympic Games will feature 32 major events and 329 minor events. The China delegation sent 405 athletes to compete. |
| xunjie | At the 2024 Paris Olympics, there were 32 major events and 329 minor events. The Chinese delegation sent 405 athletes to the competition. |

### 场景 4：多段落叙事

> 春天来了，万物复苏。小河边的柳树抽出了嫩绿的新芽，微风吹过，柳条轻轻摇摆。
> 田野里，农民们开始忙碌起来。他们翻土、播种，期待着秋天的丰收。
> 孩子们在草地上奔跑嬉戏，欢笑声回荡在整个村庄。

| 引擎 | 译文（摘要） |
|------|-------------|
| iflyrec | ...The willows beside the river sprouted tender green buds, and the breeze blew... The children ran and played on the grass... |
| caiyun | ...The Willows by the river bring forth new green shoots, and the willows sway gently in the breeze... Children run and play on the grass... |
| sogou | ...the breeze is blowing, and the wicker is swaying gently... The children were running and playing on the grass... |
| bing | ...The willow trees by the small river have sprouted tender green buds, and as the breeze passes, the willow branches sway gently... Children run and play on the grass... |
| itranslate | ...the breeze blowing through, the willow swings gently... The children ran on the grass and laughter echoed throughout the village. |
| qqtransmart | ...The willow trees by the river sprouted tender green buds. The breeze blew and the willow trees swayed gently... Children ran and played on the grass... |
| xunjie | ...the breeze blew, and the willows swayed gently... The children ran and played on the grass, and laughs echoed throughout the village. |

### 场景 5：英译中

> Artificial intelligence is transforming the way we live and work. Machine learning models can now understand natural language, generate images, and even write code.

| 引擎 | 译文 |
|------|------|
| iflyrec | 人工智能正在改变我们的生活和工作方式。机器学习模型现在可以理解自然语言，生成图像，甚至编写代码。 |
| caiyun | 人工智能正在改变我们的生活和工作方式。机器学习模型现在可以理解自然语言，生成图像，甚至编写代码。 |
| sogou | 人工智能正在改变我们的生活和工作方式。机器学习模型现在可以理解自然语言，生成图像，甚至编写代码。 |
| bing | 人工智能正在改变我们的生活和工作方式。机器学习模型现在可以理解自然语言、生成图像，甚至编写代码。 |
| itranslate | 人工智能正在改变我们的生活和工作方式。 机器学习模型现在可以理解自然语言，生成图像，甚至编写代码。 |
| qqtransmart | 人工智能正在改变我们的生活和工作方式。 机器学习模型现在可以理解自然语言，生成图像，甚至编写代码。 |
| xunjie | 人工智能正在改变我们的生活和工作方式。 机器学习模型现在可以理解自然语言，生成图像，甚至编写代码。 |

> 注：以上结果来自实际 API 调用，不同时间调用可能略有差异。部分引擎（alibaba、translatecom 等）因 API 偶发不稳定未列入。iciba 因 AES 解密 bug 待修复、youdao 因签名机制变更待修复。

## 项目结构

```
translator/
├── go.mod              # module: github.com/zzdylan/translator
├── translator.go       # 统一接口定义
├── httpclient.go       # 公共 HTTP 客户端、Headers 工具函数
├── driver/
│   ├── iflyrec/        # ✅ 讯飞听见
│   ├── caiyun/         # ✅ 彩云小译
│   ├── sogou/          # ✅ 搜狗翻译
│   ├── iciba/          # ✅ 金山词霸
│   ├── alibaba/        # ✅ 阿里翻译
│   ├── youdao/         # ✅ 有道翻译
│   ├── papago/         # ✅ Papago
│   ├── modernmt/       # ✅ ModernMT
│   ├── lara/           # ✅ Lara
│   ├── lingvanex/      # ✅ Lingvanex
│   ├── translatecom/   # ✅ Translate.com
│   ├── yandex/         # ✅ Yandex
│   ├── bing/           # ✅ Bing
│   ├── itranslate/     # ✅ iTranslate
│   ├── qqtransmart/    # ✅ 腾讯交互翻译
│   ├── reverso/        # ⛔ Cloudflare 阻断，暂不可用
│   └── xunjie/         # ✅ 迅捷翻译
└── example/
    └── main.go         # 使用示例
```
