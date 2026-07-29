package reportvalue

import "testing"

func TestCompareUnchangedChineseMarkdown(t *testing.T) {
	generated := "## 工作概览  \r\n1. 完成项目 A 联调。\r\n\r\n## 工作详情\r\n### 项目 A\r\n完成接口。\r\n"
	user := "## 工作概览\n1. 完成项目 A 联调。\n\n## 工作详情\n### 项目 A\n完成接口。"
	got := Compare(generated, user)
	if got.Text.ChangeBand != ChangeUnchanged || got.Text.DiffRatio == nil || *got.Text.DiffRatio != 0 {
		t.Fatalf("unexpected text metrics: %#v", got.Text)
	}
	if got.Summary.Outcome != "summary_unchanged" {
		t.Fatalf("summary outcome = %q", got.Summary.Outcome)
	}
	if len(got.Topics.Generated) != 1 || got.Topics.Generated[0] != "工作详情" {
		t.Fatalf("topics = %#v", got.Topics)
	}
}

func TestCompareSummaryRemovedAndTopicChanges(t *testing.T) {
	generated := "## 工作概览\n总结内容\n\n## 项目 A\n完成接口"
	user := "## 项目 B\n补充文档"
	got := Compare(generated, user)
	if got.Summary.Outcome != "summary_removed" || !got.Summary.Reduced30 {
		t.Fatalf("summary = %#v", got.Summary)
	}
	if len(got.Topics.Deleted) != 1 || got.Topics.Deleted[0] != "项目 A" || len(got.Topics.Added) != 1 || got.Topics.Added[0] != "项目 B" {
		t.Fatalf("topics = %#v", got.Topics)
	}
}

func TestCompareKeepsLegacyWorkSummaryCompatible(t *testing.T) {
	generated := "## 项目 A\n完成接口\n\n## 工作总结\n旧版总结内容"
	user := "## 项目 A\n完成接口\n\n## 工作总结\n旧版总结内容"
	got := Compare(generated, user)
	if got.Summary.Outcome != "summary_unchanged" {
		t.Fatalf("legacy summary outcome = %q", got.Summary.Outcome)
	}
	if len(got.Topics.Generated) != 1 || got.Topics.Generated[0] != "项目 A" {
		t.Fatalf("legacy topics = %#v", got.Topics)
	}
}

func TestCompareEmptyValuesAreNotApplicable(t *testing.T) {
	got := Compare("", "")
	if got.Text.DiffRatio != nil || got.Text.DraftRetention != nil || got.Text.UserAddition != nil || got.Text.ChangeBand != "not_applicable" {
		t.Fatalf("text metrics = %#v", got.Text)
	}
	if got.Summary.Outcome != "not_applicable" {
		t.Fatalf("summary = %#v", got.Summary)
	}
}

func TestCompareClassifiesLightAndMediumBoundaries(t *testing.T) {
	light := Compare("abcdefghij", "abcdefghiX")
	if light.Text.ChangeBand != ChangeLight {
		t.Fatalf("light band = %q, ratio=%v", light.Text.ChangeBand, light.Text.DiffRatio)
	}
	medium := Compare("abcdefghij", "abcdefgXYZ")
	if medium.Text.ChangeBand != ChangeMedium {
		t.Fatalf("medium band = %q, ratio=%v", medium.Text.ChangeBand, medium.Text.DiffRatio)
	}
}

func TestMyersLCSMatchesReferenceForSmallInputs(t *testing.T) {
	values := []string{"", "a", "b", "aa", "ab", "ba", "bb", "aba", "bab", "aabb", "baba"}
	for _, left := range values {
		for _, right := range values {
			got := lcsLength([]rune(left), []rune(right))
			want := referenceLCS([]rune(left), []rune(right))
			if got != want {
				t.Fatalf("lcsLength(%q, %q) = %d, want %d", left, right, got, want)
			}
		}
	}
}

func referenceLCS(left, right []rune) int {
	table := make([][]int, len(left)+1)
	for index := range table {
		table[index] = make([]int, len(right)+1)
	}
	for leftIndex := range left {
		for rightIndex := range right {
			if left[leftIndex] == right[rightIndex] {
				table[leftIndex+1][rightIndex+1] = table[leftIndex][rightIndex] + 1
			} else if table[leftIndex][rightIndex+1] > table[leftIndex+1][rightIndex] {
				table[leftIndex+1][rightIndex+1] = table[leftIndex][rightIndex+1]
			} else {
				table[leftIndex+1][rightIndex+1] = table[leftIndex+1][rightIndex]
			}
		}
	}
	return table[len(left)][len(right)]
}
