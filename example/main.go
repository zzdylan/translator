package main

import (
	"fmt"
	"sync"

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

func main() {
	engines := []translator.Translator{
		iflyrec.New(),
		lara.New(),
		translatecom.New(),
		sogou.New(),
		modernmt.New(),
		caiyun.New(),
		youdao.New(),
		papago.New(),
		iciba.New(),
		alibaba.New(),
		lingvanex.New(),
		yandex.New(),
		bing.New(),
		itranslate.New(),
		qqtransmart.New(),
		// reverso.New(), // Cloudflare Managed Challenge 保护，Python 用 cloudscraper 绕过，Go 暂无可行方案
		xunjie.New(),
	}

	text := "你好，世界"
	from := "auto"
	to := "en"

	fmt.Printf("Translating: %q (%s -> %s)\n\n", text, from, to)

	var wg sync.WaitGroup
	type result struct {
		name string
		res  *translator.TranslateResult
		err  error
	}
	results := make(chan result, len(engines))

	for _, e := range engines {
		wg.Add(1)
		go func(eng translator.Translator) {
			defer wg.Done()
			r, err := eng.Translate(text, from, to)
			results <- result{name: eng.Name(), res: r, err: err}
		}(e)
	}

	wg.Wait()
	close(results)

	for r := range results {
		if r.err != nil {
			fmt.Printf("[%s] ❌ Error: %v\n", r.name, r.err)
		} else {
			fmt.Printf("[%s] ✅ %s\n", r.name, r.res.Text)
		}
	}
}
