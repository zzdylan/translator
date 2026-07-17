package translator

// Translator 定义所有翻译引擎的统一接口。
type Translator interface {
	Translate(text, from, to string) (*TranslateResult, error)
	SupportedLanguages() []string
	Name() string
}

// TranslateResult 翻译结果。
type TranslateResult struct {
	Text   string // 翻译后的文本
	From   string // 检测到的源语言
	To     string // 目标语言
	Engine string // 引擎名称
}
