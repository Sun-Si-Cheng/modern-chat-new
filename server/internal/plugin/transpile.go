package plugin

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
)

// 插件源文件语言。插件仍是「单文件、无打包」模型：
// TypeScript 只在安装时做一次逐文件去类型，入库与下发一律是可执行 JS，
// 前端沙箱不需要任何编译能力，插件体积与首屏包体都不受影响。
const (
	LangJS = "js"
	LangTS = "ts"
)

// tsHints 用于在没有明确语言提示时判断源码像不像 TypeScript。
// 只在「明确不像 JS」时才命中最保守的几种写法，避免把普通 JS 误判成 TS。
var tsHints = []*regexp.Regexp{
	// interface Foo { / type Foo =
	regexp.MustCompile(`(?m)^\s*(?:export\s+)?(?:interface|type)\s+[A-Za-z_$][\w$]*\s*[={<]`),
	// 形参/返回值的类型标注：): Foo {  或  x: string)
	regexp.MustCompile(`\)\s*:\s*[A-Za-z_$][\w$<>\[\]| ,.]*\s*(?:=>|\{)`),
	regexp.MustCompile(`\b(?:const|let|var)\s+[A-Za-z_$][\w$]*\s*:\s*[A-Za-z_$][\w$<>\[\]|]*\s*=`),
	// 类型断言 / 泛型实参
	regexp.MustCompile(`\bas\s+(?:const|[A-Za-z_$][\w$]*(?:\[\])?)\b`),
}

// LooksLikeTS 源码是否带明显的 TypeScript 语法特征。
func LooksLikeTS(code string) bool {
	for _, re := range tsHints {
		if re.MatchString(code) {
			return true
		}
	}
	return false
}

// LangFromFilename 按文件名后缀推断语言（.ts / .mts → ts，其余 js）。
func LangFromFilename(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	if strings.HasSuffix(n, ".ts") || strings.HasSuffix(n, ".mts") {
		return LangTS
	}
	return LangJS
}

// manifestComment 取出源码顶部的 /* {...} */ 注释原文（含首尾符号）。
func manifestComment(code string) string {
	start := strings.Index(code, "/*")
	if start < 0 {
		return ""
	}
	end := strings.Index(code[start:], "*/")
	if end < 0 {
		return ""
	}
	return code[start : start+end+2]
}

// Transpile 用 esbuild 把 TypeScript 转成 ES2020 JavaScript。
// 只做逐文件去类型与语法降级，不做打包、不解析 import（插件是单文件模型）。
func Transpile(code string) (string, error) {
	res := api.Transform(code, api.TransformOptions{
		Loader:        api.LoaderTS,
		Target:        api.ES2020,
		KeepNames:     true,
		LegalComments: api.LegalCommentsInline,
		Charset:       api.CharsetUTF8,
	})
	if len(res.Errors) > 0 {
		e := res.Errors[0]
		if e.Location != nil {
			return "", fmt.Errorf("%w: TypeScript 转译失败（第 %d 行 %d 列）: %s",
				ErrInvalid, e.Location.Line, e.Location.Column, e.Text)
		}
		return "", fmt.Errorf("%w: TypeScript 转译失败: %s", ErrInvalid, e.Text)
	}
	return string(res.Code), nil
}

// Compile 校验 manifest，并在源码是 TypeScript 时转译成 JS。
// 返回「可以入库、可以下发」的代码与解析出的 manifest。
//
// manifest 一律从**源码**解析：转译可能移动注释位置，先解析再转译更稳；
// 转译产物若丢失了 manifest 注释，则把原注释补回顶部，保证下次按同名升级时仍可解析。
func Compile(code string, lang string) (string, *Manifest, error) {
	m, err := Parse(code)
	if err != nil {
		return "", nil, err
	}
	isTS := lang == LangTS || (lang == "" && LooksLikeTS(code))
	if !isTS {
		return code, m, nil
	}
	js, err := Transpile(code)
	if err != nil {
		return "", nil, err
	}
	if _, err := Parse(js); err != nil {
		if comment := manifestComment(code); comment != "" {
			js = comment + "\n" + js
		}
		if _, err := Parse(js); err != nil {
			return "", nil, fmt.Errorf("%w: 转译后 manifest 丢失", ErrInvalid)
		}
	}
	return js, m, nil
}
