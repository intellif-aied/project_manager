package reportmemory

import "testing"

func TestExtractThemesPrefersOverviewHierarchy(t *testing.T) {
	themes := ExtractThemes(`# 工作总结

## 工作概览

1. KV Cache 压缩算法研发
   - OSCAR 算法适配
   - 1-bit KV Cache 验证
2. Knowledge Map
`)
	if len(themes) != 2 {
		t.Fatalf("expected 2 themes, got %#v", themes)
	}
	if themes[0].Title != "KV Cache 压缩算法研发" || len(themes[0].Children) != 2 {
		t.Fatalf("unexpected first theme: %#v", themes[0])
	}
}

func TestExtractThemesKeepsOverviewAsPrimarySource(t *testing.T) {
	themes := ExtractThemes(`## 工作概览

1. 完成 Knowledge Map 产品判断与 Skill 落地，相关验证通过。

## 工作详情

### Knowledge Map

- 完成产品判断
- 落地 knowledge-map-search Skill
`)
	if len(themes) != 1 || themes[0].Title != "完成 Knowledge Map 产品判断与 Skill 落地，相关验证通过。" {
		t.Fatalf("expected overview item, got %#v", themes)
	}
}

func TestNormalizeNamePreservesTechnicalTermsWithoutPunctuation(t *testing.T) {
	if got := normalizeName("Knowledge Map / RTL 优化"); got != "knowledgemaprtl优化" {
		t.Fatalf("unexpected normalized name: %q", got)
	}
}
