package reporteval

import "testing"

func TestMeasureReportShapeClassifiesCommonManualFormat(t *testing.T) {
	shape := measureReportShape("1. 完成协议设计\n2. 验证 Report MCP 写入流程")
	if shape.FormatClass != "ordered_list" || shape.OrderedItems != 2 || shape.LineCount != 2 {
		t.Fatalf("shape = %#v", shape)
	}
}

func TestPatternComparisonIsDescriptive(t *testing.T) {
	comparison := comparePatternDistribution(PatternRange{P50: 100, P90: 400}, []int{10, 20, 30})
	if comparison.GeneratedP50 != 20 || comparison.GeneratedP90 != 30 || comparison.DeltaP50 != -80 {
		t.Fatalf("comparison = %#v", comparison)
	}
}
