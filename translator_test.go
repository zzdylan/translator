package translator_test

import (
	"strings"
	"testing"
	"time"

	translator "github.com/zzdylan/translator"
	"github.com/zzdylan/translator/driver/alibaba"
	"github.com/zzdylan/translator/driver/bing"
	"github.com/zzdylan/translator/driver/caiyun"
	"github.com/zzdylan/translator/driver/iciba"
	"github.com/zzdylan/translator/driver/iflyrec"
	"github.com/zzdylan/translator/driver/itranslate"
	"github.com/zzdylan/translator/driver/lara"
	"github.com/zzdylan/translator/driver/lingvanex"
	"github.com/zzdylan/translator/driver/modernmt"
	"github.com/zzdylan/translator/driver/papago"
	"github.com/zzdylan/translator/driver/qqtransmart"

	// "github.com/zzdylan/translator/driver/reverso" // Cloudflare 保护，暂不可用
	"github.com/zzdylan/translator/driver/sogou"
	"github.com/zzdylan/translator/driver/translatecom"
	"github.com/zzdylan/translator/driver/xunjie"
	"github.com/zzdylan/translator/driver/yandex"
	"github.com/zzdylan/translator/driver/youdao"
)

// TestSingleTranslator 演示如何使用单个翻译驱动进行翻译。
// 可选驱动:
//
//	iflyrec(讯飞听见), caiyun(彩云小译), sogou(搜狗翻译), iciba(金山词霸),
//	alibaba(阿里翻译), youdao(有道翻译), papago(Naver Papago),
//	modernmt(ModernMT), lara(Lara), lingvanex(Lingvanex),
//	translatecom(Translate.com), yandex(Yandex),
//	bing(必应翻译), itranslate(iTranslate), qqtransmart(腾讯交互翻译),
//	reverso(Reverso), xunjie(迅捷翻译)
//
// 示例: 将下方 lingvanex.New() 替换为 caiyun.New() 即可切换驱动。
func TestSingleTranslator(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过集成测试（short 模式）")
	}

	engine := iflyrec.New()
	t.Logf("引擎: %s", engine.Name())
	t.Logf("支持语言数: %d", len(engine.SupportedLanguages()))

	result, err := engine.Translate("今天天气怎么样，你在哪里玩呢", "auto", "en")
	if err != nil {
		t.Fatalf("翻译失败: %v", err)
	}

	t.Logf("翻译结果: %s", result.Text)
	t.Logf("源语言: %s -> 目标语言: %s", result.From, result.To)
	t.Logf("引擎: %s", result.Engine)
}

// TestTranslators 集成测试：验证所有翻译驱动能正常调用真实 API 完成翻译。
// 使用 -short 标志可跳过此测试。
func TestTranslators(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过集成测试（short 模式）")
	}

	drivers := []translator.Translator{
		iflyrec.New(),
		caiyun.New(),
		sogou.New(),
		iciba.New(),
		alibaba.New(),
		youdao.New(), // 注意：有道签名机制已变更，当前驱动需要修复
		papago.New(),
		modernmt.New(),
		lara.New(),
		lingvanex.New(),
		translatecom.New(),
		yandex.New(),
		bing.New(),
		itranslate.New(),
		qqtransmart.New(),
		// reverso.New(), // Cloudflare Managed Challenge 保护，Python 用 cloudscraper 绕过，Go 暂无可行方案
		xunjie.New(),
	}

	for _, drv := range drivers {
		drv := drv
		t.Run(drv.Name(), func(t *testing.T) {
			// 验证驱动名称
			name := drv.Name()
			if name == "" {
				t.Fatal("Name() 返回了空字符串")
			}

			// 验证翻译功能（会触发初始化，包括动态获取语言列表）
			result, err := drv.Translate("你好，世界", "auto", "en")
			if err != nil {
				t.Fatalf("Translate() 失败: %v", err)
			}
			if result.Text == "" {
				t.Error("Translate() 返回了空的翻译结果")
			}
			if result.Engine != name {
				t.Errorf("Engine = %q, 期望 %q", result.Engine, name)
			}
			if result.To != "en" {
				t.Errorf("To = %q, 期望 %q", result.To, "en")
			}
			t.Logf("翻译结果: %+v", result)

			// 验证支持的语言列表（初始化后动态获取的）
			langs := drv.SupportedLanguages()
			if len(langs) == 0 {
				t.Fatal("SupportedLanguages() 返回了空列表")
			}
			t.Logf("支持语言数: %d", len(langs))

			// 驱动间延迟，避免触发限流
			time.Sleep(500 * time.Millisecond)
		})
	}
}

// TestTranslationQuality 使用多段文本测试各引擎的翻译质量。
// 包含多种场景：日常对话、技术文档、古诗词、成语、数字混合。
// 使用 -short 标志可跳过此测试。
func TestTranslationQuality(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过集成测试（short 模式）")
	}

	type testCase struct {
		name     string
		text     string
		from     string
		to       string
		keywords [][]string // 翻译结果中应包含的关键词组（每组任一匹配即可，不区分大小写）
	}

	// 测试场景，控制在 500 字符以内（兼容 xunjie/lara 的限制）
	cases := []testCase{
		{
			name: "日常对话",
			text: "今天天气真好，我打算去公园散步。下午三点我们在咖啡厅见面，你觉得怎么样？",
			from: "auto",
			to:   "en",
			keywords: [][]string{
				{"park"},
				{"coffee", "cafe", "café"},
			},
		},
		{
			name: "技术文档",
			text: "Go语言是一种静态类型、编译型的编程语言。它由Google开发，具有高效的并发处理能力。goroutine是Go语言中轻量级的线程实现。",
			from: "auto",
			to:   "en",
			keywords: [][]string{
				{"go"},
				{"google"},
				{"goroutine"},
			},
		},
		{
			name: "数字与专有名词混合",
			text: "2024年巴黎奥运会共设32个大项、329个小项。中国代表团派出了405名运动员参赛。",
			from: "auto",
			to:   "en",
			keywords: [][]string{
				{"2024"},
				{"paris"},
				{"olympic"},
				{"china", "chinese"},
			},
		},
		{
			name: "多段落叙事",
			text: "春天来了，万物复苏。小河边的柳树抽出了嫩绿的新芽，微风吹过，柳条轻轻摇摆。\n田野里，农民们开始忙碌起来。他们翻土、播种，期待着秋天的丰收。\n孩子们在草地上奔跑嬉戏，欢笑声回荡在整个村庄。",
			from: "auto",
			to:   "en",
			keywords: [][]string{
				{"spring"},
				{"wind", "breeze"},
				{"children", "kids"},
			},
		},
		{
			name: "英译中",
			text: "Artificial intelligence is transforming the way we live and work. Machine learning models can now understand natural language, generate images, and even write code.",
			from: "en",
			to:   "zh",
			keywords: [][]string{
				{"人工智能"},
				{"机器学习"},
			},
		},
	}

	drivers := []translator.Translator{
		iflyrec.New(),
		caiyun.New(),
		sogou.New(),
		iciba.New(),
		alibaba.New(),
		// youdao.New(), // 签名机制已变更，跳过
		papago.New(),
		modernmt.New(),
		lara.New(),
		lingvanex.New(),
		translatecom.New(),
		yandex.New(),
		bing.New(),
		itranslate.New(),
		qqtransmart.New(),
		xunjie.New(),
	}

	for _, drv := range drivers {
		drv := drv
		t.Run(drv.Name(), func(t *testing.T) {
			for _, tc := range cases {
				tc := tc
				t.Run(tc.name, func(t *testing.T) {
					result, err := drv.Translate(tc.text, tc.from, tc.to)
					if err != nil {
						t.Fatalf("翻译失败: %v", err)
					}

					if result.Text == "" {
						t.Fatal("翻译结果为空")
					}

					t.Logf("原文: %s", tc.text)
					t.Logf("译文: %s", result.Text)

					// 检查关键词是否出现在翻译结果中（每组任一匹配即可）
					lower := strings.ToLower(result.Text)
					for _, group := range tc.keywords {
						found := false
						for _, kw := range group {
							if strings.Contains(lower, strings.ToLower(kw)) {
								found = true
								break
							}
						}
						if !found {
							t.Errorf("译文中缺少关键词组: %v", group)
						}
					}

					time.Sleep(500 * time.Millisecond)
				})
			}
		})
	}
}
