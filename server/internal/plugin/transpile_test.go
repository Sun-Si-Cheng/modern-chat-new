package plugin

import (
	"strings"
	"testing"
)

const tsSample = `/* {
  "name": "ts-demo",
  "version": "1.0.0",
  "description": "TS 示例",
  "permissions": ["ui:panel"],
  "allowedHosts": []
} */

interface Weather { city: string; temp: number }

function label(w: Weather): string {
  return w.city + ' ' + w.temp + '℃'
}

function __mcActivate(api: any): void {
  const w: Weather = { city: '杭州', temp: 22 }
  api.addSidePanel({ kind: 'panel', title: label(w), body: [] })
}
`

func TestLooksLikeTS(t *testing.T) {
	if !LooksLikeTS(tsSample) {
		t.Fatal("TS 示例应被识别为 TypeScript")
	}
	plain := "/* {\"name\":\"a\",\"version\":\"1.0.0\",\"description\":\"\",\"permissions\":[\"ui:panel\"],\"allowedHosts\":[]} */\nfunction __mcActivate(api){ api.on('ui:action', function(p){ return p }) }\n"
	if LooksLikeTS(plain) {
		t.Fatal("普通 JS 不应被识别为 TypeScript")
	}
}

func TestLangFromFilename(t *testing.T) {
	cases := map[string]string{
		"a.ts": LangTS, "a.mts": LangTS, "A.TS": LangTS,
		"a.js": LangJS, "a.mjs": LangJS, "": LangJS,
	}
	for name, want := range cases {
		if got := LangFromFilename(name); got != want {
			t.Fatalf("LangFromFilename(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestCompileTypeScript(t *testing.T) {
	js, m, err := Compile(tsSample, LangTS)
	if err != nil {
		t.Fatalf("Compile 失败: %v", err)
	}
	if m.Name != "ts-demo" {
		t.Fatalf("manifest name = %q", m.Name)
	}
	// 转译后不应残留类型标注与 interface 声明
	for _, bad := range []string{"interface ", ": string", ": Weather", ": any"} {
		if strings.Contains(js, bad) {
			t.Fatalf("转译产物仍含 TS 语法 %q:\n%s", bad, js)
		}
	}
	if !strings.Contains(js, "__mcActivate") {
		t.Fatal("转译产物缺少入口函数")
	}
	// 入库代码必须仍能解析出 manifest（同名升级依赖它）
	if _, err := Parse(js); err != nil {
		t.Fatalf("转译产物 manifest 不可解析: %v", err)
	}
}

func TestCompilePlainJSKeepsSource(t *testing.T) {
	src := "/* {\"name\":\"p\",\"version\":\"1.0.0\",\"description\":\"\",\"permissions\":[\"ui:panel\"],\"allowedHosts\":[]} */\nfunction __mcActivate(api){ api.storage.set('k','v') }\n"
	out, _, err := Compile(src, LangJS)
	if err != nil {
		t.Fatalf("Compile 失败: %v", err)
	}
	if out != src {
		t.Fatal("普通 JS 不应被改写")
	}
}

func TestCompileRejectsBrokenTS(t *testing.T) {
	broken := "/* {\"name\":\"b\",\"version\":\"1.0.0\",\"description\":\"\",\"permissions\":[\"ui:panel\"],\"allowedHosts\":[]} */\nfunction __mcActivate(api: { api: ) }\n"
	if _, _, err := Compile(broken, LangTS); err == nil {
		t.Fatal("语法错误的 TS 应返回错误")
	}
}
