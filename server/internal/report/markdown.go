// markdown.go 实现 RiskReport 的确定性 Markdown 渲染。
//
// 渲染器不改变报告语义：只读取已冻结的报告字段并排版，不做二次加工、
// 不引入新的泄露面（上游构建链保证 Evidence/Fact 不含原始 Secret 与未脱敏
// 代码）。相同报告实例的渲染结果逐字节相同——段落顺序固定、维度按构建时
// 的稳定顺序输出、时间统一格式化为 UTC RFC3339、表格单元格内的竖线转义，
// 因此可直接作为 golden 快照比对的输入。
//
// 段落顺序遵循 spec/01-product-requirements.md 第 5.2 节推荐顺序：
// 状态与级别 → 一句话结论 → 变更概览 → 风险维度 → 需要优先确认的发现 →
// 建议补充的测试 → 分析范围与降级原因 → 协议版本信息。
package report

import (
	"fmt"
	"strings"

	"change-risk-analyzer/internal/domain"
)

// RenderMarkdown 把报告渲染为稳定的 Markdown 字符串。
func RenderMarkdown(rep *domain.RiskReport) (string, error) {
	if rep == nil {
		return "", fmt.Errorf("report: 报告为 nil，无法渲染 Markdown")
	}

	var b strings.Builder
	b.WriteString("# 变更风险分析报告\n\n")
	b.WriteString(fmt.Sprintf("- 状态：`%s`\n", rep.Status))
	b.WriteString(fmt.Sprintf("- 总体风险：**%s**（%d / 100 分）\n", rep.OverallLevel, rep.OverallScore))
	b.WriteString(fmt.Sprintf("- 一句话结论：%s\n", overallConclusion(rep)))
	b.WriteString(fmt.Sprintf("- 仓库：`%s` · PR #%d · head SHA `%s`\n",
		rep.Request.Repository.FullName, rep.Request.PullRequestNumber, rep.Request.HeadSHA))
	b.WriteString(fmt.Sprintf("- 生成时间：%s（UTC）\n", rep.GeneratedAt.UTC().Format("2006-01-02T15:04:05Z")))

	writeChangeOverview(&b, rep.ChangeSummary)
	writeDimensions(&b, rep.Dimensions)
	writeFindings(&b, rep.Findings)
	writeTestGaps(&b, rep.TestGaps)
	writeDegradationReasons(&b, rep.DegradationReasons)

	b.WriteString("\n## 输出信息\n\n")
	b.WriteString(fmt.Sprintf("- schema 版本：`%s`\n", rep.SchemaVersion))
	b.WriteString(fmt.Sprintf("- analyzer 版本：`%s`\n", singleLine(rep.AnalyzerVersion)))
	return b.String(), nil
}

// overallConclusion 用固定模板生成一句话结论，避免自由文本影响确定性。
func overallConclusion(rep *domain.RiskReport) string {
	if len(rep.Findings) == 0 {
		return "本次变更未产生需要优先确认的风险发现。"
	}
	return fmt.Sprintf("共列出 %d 条需要人工复核的候选发现，总体级别 %s。",
		len(rep.Findings), rep.OverallLevel)
}

func writeChangeOverview(b *strings.Builder, s domain.ChangeSummary) {
	b.WriteString("\n## 变更概览\n\n")
	b.WriteString("| 指标 | 值 |\n")
	b.WriteString("| --- | --- |\n")
	b.WriteString(fmt.Sprintf("| 文件数（已见 / 已分析） | %d / %d |\n", s.FilesSeen, s.FilesAnalyzed))
	b.WriteString(fmt.Sprintf("| 新增行 | %d |\n", s.Additions))
	b.WriteString(fmt.Sprintf("| 删除行 | %d |\n", s.Deletions))
	truncated := "否"
	if s.Truncated {
		truncated = "是"
	}
	b.WriteString(fmt.Sprintf("| 变更内容被截断 | %s |\n", truncated))
	if len(s.TruncationReasons) > 0 {
		for _, reason := range s.TruncationReasons {
			b.WriteString(fmt.Sprintf("- 截断原因：%s\n", reason))
		}
	}
}

func writeDimensions(b *strings.Builder, dims []domain.RiskDimension) {
	b.WriteString("\n## 风险维度\n\n")
	if len(dims) == 0 {
		b.WriteString("本轮没有任何维度信号。\n")
		return
	}
	b.WriteString("| 维度 | 得分 | 级别 | 线索数 | 说明 |\n")
	b.WriteString("| --- | --- | --- | --- | --- |\n")
	for _, d := range dims {
		b.WriteString(fmt.Sprintf("| %s | %d | %s | %d | %s |\n",
			escapeCell(string(d.Category)), d.Score, d.Level, d.SignalCount, escapeCell(d.Summary)))
	}
}

func writeFindings(b *strings.Builder, findings []domain.Finding) {
	b.WriteString("\n## 需要优先确认的发现\n\n")
	if len(findings) == 0 {
		b.WriteString("本轮没有需要人工优先确认的发现。\n")
		return
	}
	for i, f := range findings {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(fmt.Sprintf("### %d. %s\n\n", i+1, singleLine(f.Title)))
		b.WriteString(fmt.Sprintf("- 类别：`%s` · 严重度：**%s** · 来源：`%s`\n", f.Category, f.Severity, f.Source))
		b.WriteString(fmt.Sprintf("- 规则：%s\n", strings.Join(f.RuleIDs, ", ")))
		for _, e := range f.Evidence {
			b.WriteString(fmt.Sprintf("- 证据：%s — %s\n", evidenceLocation(e), escapeCell(e.Fact)))
		}
		b.WriteString(fmt.Sprintf("- 影响：%s\n", singleLine(f.Impact)))
		b.WriteString(fmt.Sprintf("- 建议：%s\n", singleLine(f.Recommendation)))
	}
}

// evidenceLocation 把证据渲染为「文件:行号」位置；文件级证据只显示路径，
// 行级证据显示起始行或起止范围，与领域层 side/行号不变式一一对应。
// 路径以反引号包裹，行号与侧别标注在反引号之外。
func evidenceLocation(e domain.Evidence) string {
	switch e.Side {
	case domain.SideLeft, domain.SideRight:
		side := "左侧"
		if e.Side == domain.SideRight {
			side = "右侧"
		}
		if e.StartLine == e.EndLine {
			return fmt.Sprintf("`%s`:%d（%s）", e.File, e.StartLine, side)
		}
		return fmt.Sprintf("`%s`:%d-%d（%s）", e.File, e.StartLine, e.EndLine, side)
	default:
		return fmt.Sprintf("`%s`（文件级）", e.File)
	}
}

func writeTestGaps(b *strings.Builder, gaps []domain.TestGap) {
	b.WriteString("\n## 建议补充的测试\n\n")
	if len(gaps) == 0 {
		b.WriteString("本轮没有产生测试缺口建议。\n")
		return
	}
	for _, g := range gaps {
		b.WriteString(fmt.Sprintf("- %s（优先级 %s）：%s 建议验证：%s\n",
			singleLine(g.Area), g.Priority, singleLine(g.Reason), singleLine(g.RecommendedTest)))
	}
}

func writeDegradationReasons(b *strings.Builder, reasons []domain.DegradationReason) {
	b.WriteString("\n## 分析范围与降级原因\n\n")
	if len(reasons) == 0 {
		b.WriteString("本次分析完整执行，没有降级事项。\n")
		return
	}
	for _, r := range reasons {
		entry := fmt.Sprintf("- `%s`：%s", r.Code, singleLine(r.Message))
		if r.Retryable {
			entry += "（可重试）"
		}
		b.WriteString(entry + "\n")
	}
}

// escapeCell 转义表格单元格文本：换行折叠为空格防止破坏表格结构，
// 竖线转义为 `\|` 防止被解析为列分隔符。
func escapeCell(text string) string {
	replaced := singleLine(text)
	replacer := strings.NewReplacer("|", "\\|")
	return replacer.Replace(replaced)
}

// singleLine 把多行文本折叠为单行，用于标题与列表项的防御性规范化；
// 正常输入不含换行，该函数保证恶意或异常输入不会破坏渲染结构。
func singleLine(text string) string {
	replacer := strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ")
	return replacer.Replace(text)
}
