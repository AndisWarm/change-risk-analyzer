// policy.go 实现确定性策略引擎：把信号运行器聚合输出的 RiskSignal 集合
// 转换为经过证据校验的 Finding，按 spec/02-risk-model.md 第 5 节的初始
// 评分配置计算维度分数与总分，并给出默认门禁建议（不阻塞合并）。
//
// 本包是纯函数实现：无网络、无文件系统、无全局可变状态；内部先对输入
// 做规范化排序再累加浮点贡献分，因此同一输入无论以何种顺序到达，重复
// 求值结果都完全一致（reflect.DeepEqual 级别稳定）。
//
// 与规格的逐条对应关系：
//   - 总分公式与阈值（spec/02 第 5 节）：
//     raw_score = Σ(signal_weight × evidence_factor × exposure_factor)
//     mitigated_score = max(0, raw_score - Σ(mitigation_credit))
//     final_score = min(100, mitigated_score)
//     0-24 low / 25-49 medium / 50-74 high / 75-100 critical 由
//     domain.LevelFromScore 统一实现，本包不重复定义阈值。
//   - evidence_factor（spec/02 第 5 节）：有行级事实 1.0，文件级事实
//     0.7，只有模型线索 0.3。
//   - exposure_factor（spec/02 第 5 节）：规格只给出取值方向（公共接口/
//     生产配置/权限路径可提高到 1.0，内部实验代码可为 0.5），未定义
//     缺省值。本切片缺少区分公共与内部的上下文信号，固定为 1.0 的中性
//     口径（不放大也不缩小分数）。这是已记录的解释口径，真实暴露度
//     判定留待 Phase 4 调优切片补充。
//   - mitigation_credit（spec/02 第 5 节）：规格列举了可抵扣项但未给出
//     数值目录。本切片保持公式的结构性减项（当前恒为 0），不发明抵扣值；
//     RiskSignal.MitigationIDs 仅随输入保留，不影响分数。
//   - 单条 Finding 的 severity：spec/02 未单独定义 finding 级严重度映射。
//     本实现把第 5 节的单信号贡献分 round(weight × evidence_factor ×
//     exposure_factor) 经同一 LevelFromScore 阈值函数映射为级别，未引入
//     任何新常量。该口径使单个弱线索（现有权重 15-35）天然落在 low 或
//     medium，符合 spec/07 第 5 节「单个弱线索不应直接制造 high」的组合
//     约束；显式的多信号组合升级（权限扩大+不可信执行等）属于 Phase 4
//     调优范围，不在本切片实现。
//   - 维度分数（spec/02 第 2、5 节 + spec/04 第 3 节）：规格只给出总分
//     公式；维度分数按同一公式作用于该类别的信号子集求和，并按领域校验
//     要求封顶到 [0, 100]，级别同样由 LevelFromScore 得出。没有信号的
//     维度按 spec/04 第 3 节「可以省略」处理，仅输出涉及维度并经
//     domain.SortDimensions 固定排序。
//   - 门禁（spec/02 第 7 节）：MVP 默认不阻塞合并，ShouldBlock 恒为
//     false；可配置门禁属后续切片。
package policy

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"change-risk-analyzer/internal/domain"
)

// 评分常量，均来自 spec/02-risk-model.md 第 5 节的建议因子。
const (
	evidenceFactorLine      = 1.0 // 行级事实
	evidenceFactorFile      = 0.7 // 文件级事实
	evidenceFactorModelOnly = 0.3 // 只有模型线索
	exposureFactorDefault   = 1.0 // 缺省暴露度口径（见包注释）
	maxSignalWeight         = 40  // signal_weight 合法上限
)

// gateReasonDefault 是默认门禁建议的理由说明，固定不阻塞合并。
const gateReasonDefault = "默认门禁关闭：MVP 阶段只报告不阻塞合并" +
	"（spec/02-risk-model.md 第 7 节）；策略显式启用门禁属 Phase 4 可选门禁切片。"

// Result 是策略引擎对一个信号集合的确定性评估输出。
type Result struct {
	Findings     []domain.Finding       // 经证据校验的发现，已按 domain.SortFindings 排序
	Dimensions   []domain.RiskDimension // 仅含涉及维度，已按 domain.SortDimensions 排序
	OverallScore int                    // final_score，范围 [0, 100]
	OverallLevel domain.Severity        // 由 domain.LevelFromScore(OverallScore) 得出
	ShouldBlock  bool                   // 默认门禁建议，本切片恒为 false
	GateReason   string                 // 门禁建议理由，恒为 gateReasonDefault
}

// Evaluate 把聚合后的确定性信号集合转换为 Findings 并计算分数。
//
// 证据校验红线：任何一条 Evidence 未通过 domain 校验时立即返回错误，
// 错误信息包含规则 ID 与文件路径，绝不静默跳过；权重超出 spec/02 规定的
// 1-40 范围或没有任何证据的信号同样被拒绝。
func Evaluate(signals []domain.RiskSignal) (Result, error) {
	ordered := canonicalOrder(signals)

	findings := make([]domain.Finding, 0, len(ordered))
	categoryContributions := make(map[domain.RiskCategory]float64)
	categoryCounts := make(map[domain.RiskCategory]int)
	var totalContribution float64

	for _, signal := range ordered {
		if len(signal.Evidence) == 0 {
			return Result{}, fmt.Errorf("policy: signal %s 没有任何证据，无法转换为 finding", signal.RuleID)
		}
		// 证据校验红线先行：逐条校验并在失败时携带规则 ID 与文件路径。
		evidence, err := validatedEvidenceCopy(signal)
		if err != nil {
			return Result{}, err
		}
		if err := signal.Validate(); err != nil {
			return Result{}, fmt.Errorf("policy: signal %s 校验失败: %w", signal.RuleID, err)
		}
		if signal.Weight < 1 || signal.Weight > maxSignalWeight {
			return Result{}, fmt.Errorf("policy: signal %s weight %d 超出 spec/02 允许的 1-%d 范围",
				signal.RuleID, signal.Weight, maxSignalWeight)
		}

		factor := evidenceFactorOf(signal)
		contribution := float64(signal.Weight) * factor * exposureFactorDefault
		findings = append(findings, buildFinding(signal, evidence, contribution))

		categoryContributions[signal.Category] += contribution
		categoryCounts[signal.Category]++
		totalContribution += contribution
	}

	dimensions := buildDimensions(categoryContributions, categoryCounts)
	domain.SortFindings(findings)
	domain.SortDimensions(dimensions)

	// spec/02 第 5 节：mitigated = max(0, raw - credits)；final = min(100, mitigated)。
	// 当前没有数值化的抵扣目录，Σ(mitigation_credit) 结构性为 0。
	finalScore := clampScore(int(math.Round(math.Max(0, totalContribution))))

	return Result{
		Findings:     findings,
		Dimensions:   dimensions,
		OverallScore: finalScore,
		OverallLevel: domain.LevelFromScore(finalScore),
		ShouldBlock:  false,
		GateReason:   gateReasonDefault,
	}, nil
}

// canonicalOrder 返回输入的副本并按与信号运行器一致的全局键排序，
// 保证浮点累加顺序与调用方传入顺序无关。
func canonicalOrder(signals []domain.RiskSignal) []domain.RiskSignal {
	ordered := append([]domain.RiskSignal(nil), signals...)
	sort.SliceStable(ordered, func(i, j int) bool {
		left, right := policySortKey(ordered[i]), policySortKey(ordered[j])
		if left.category != right.category {
			return left.category < right.category
		}
		if left.file != right.file {
			return left.file < right.file
		}
		if left.line != right.line {
			return left.line < right.line
		}
		if left.side != right.side {
			return left.side < right.side
		}
		if left.ruleID != right.ruleID {
			return left.ruleID < right.ruleID
		}
		return left.fact < right.fact
	})
	return ordered
}

var categoryRank = map[domain.RiskCategory]int{
	domain.CategorySecurity:    0,
	domain.CategoryData:        1,
	domain.CategoryAPI:         2,
	domain.CategoryReliability: 3,
	domain.CategoryConcurrency: 4,
	domain.CategoryPerformance: 5,
	domain.CategoryDelivery:    6,
	domain.CategorySupplyChain: 7,
	domain.CategoryTestability: 8,
}

type policySortKeyStruct struct {
	category int
	file     string
	line     int
	side     string
	ruleID   string
	fact     string
}

func policySortKey(signal domain.RiskSignal) policySortKeyStruct {
	rank, ok := categoryRank[signal.Category]
	if !ok {
		rank = len(categoryRank)
	}
	file, line, side := "", 0, ""
	if len(signal.Evidence) > 0 {
		file = signal.Evidence[0].File
		line = signal.Evidence[0].StartLine
		side = string(signal.Evidence[0].Side)
	}
	return policySortKeyStruct{
		category: rank,
		file:     file,
		line:     line,
		side:     side,
		ruleID:   signal.RuleID,
		fact:     signal.Fact,
	}
}

// validatedEvidenceCopy 逐条校验证据并在任一条失败时返回携带规则 ID 与
// 文件路径的错误；全部通过时返回深拷贝，避免外部修改污染结果。
func validatedEvidenceCopy(signal domain.RiskSignal) ([]domain.Evidence, error) {
	copied := make([]domain.Evidence, 0, len(signal.Evidence))
	for i := range signal.Evidence {
		evidence := signal.Evidence[i]
		if err := evidence.Validate(); err != nil {
			return nil, fmt.Errorf("policy: signal %s 的第 %d 条证据非法（file %q）: %w",
				signal.RuleID, i, evidence.File, err)
		}
		copied = append(copied, evidence)
	}
	return copied, nil
}

// evidenceFactorOf 按 spec/02 第 5 节返回证据因子：
// 有行级事实为 1.0，否则文件级事实为 0.7，只有模型线索（无行级/文件级
// 事实的 model 来源）为 0.3。
func evidenceFactorOf(signal domain.RiskSignal) float64 {
	hasFileSide := false
	for i := range signal.Evidence {
		evidence := signal.Evidence[i]
		if (evidence.Side == domain.SideLeft || evidence.Side == domain.SideRight) &&
			evidence.StartLine >= 1 && evidence.EndLine >= 1 {
			return evidenceFactorLine
		}
		if evidence.Side == domain.SideFile {
			hasFileSide = true
		}
	}
	if hasFileSide {
		return evidenceFactorFile
	}
	if signal.Source == domain.SourceModel {
		return evidenceFactorModelOnly
	}
	// 确定性信号经 Validate 后必然具备行级或文件级证据，此分支仅为防御。
	return evidenceFactorLine
}

// buildFinding 把单条信号转换为一个 Finding。
func buildFinding(signal domain.RiskSignal, evidence []domain.Evidence, contribution float64) domain.Finding {
	first := evidence[0]
	line := first.StartLine
	if line < 1 {
		line = 0
	}
	status := domain.EvidenceConfirmed
	if signal.Source == domain.SourceModel {
		status = domain.EvidenceNeedsReview
	}
	return domain.Finding{
		ID:             findingID(signal.RuleID, first.File, line),
		Category:       signal.Category,
		Severity:       domain.LevelFromScore(clampScore(int(math.Round(contribution)))),
		EvidenceStatus: status,
		Confidence:     signal.Confidence,
		Title:          truncateRunes(signal.Fact, 200),
		Impact: fmt.Sprintf("确定性规则 %s 在变更中识别出该线索，它属于待人工确认的候选事实，"+
			"不代表已证实的安全或功能缺陷；如成立，可能影响%s相关的风险面。",
			signal.RuleID, categoryNoun(signal.Category)),
		Evidence:       evidence,
		Recommendation: recommendationFor(first),
		RuleIDs:        []string{signal.RuleID},
		Source:         signal.Source,
		InlineEligible: inlineEligible(evidence),
	}
}

// findingID 生成确定性复合键 "<RuleID>:<首个证据文件>:<起始行>"。
// 领域校验要求 ID 匹配 ^[A-Za-z0-9_.:-]{1,160}$，路径中的 '/' 等
// 字符统一替换为 '_'；超出长度上限时截断路径分量。
func findingID(ruleID, file string, line int) string {
	id := ruleID + ":" + sanitizeIDComponent(file) + ":" + strconv.Itoa(line)
	const maxLen = 160
	if len(id) <= maxLen {
		return id
	}
	budget := maxLen - len(ruleID) - 1 - 1 - len(strconv.Itoa(line))
	if budget < 1 {
		budget = 1
	}
	return ruleID + ":" + truncateString(sanitizeIDComponent(file), budget) + ":" + strconv.Itoa(line)
}

// sanitizeIDComponent 把不属于 ^[A-Za-z0-9_.:-]$ 的字符替换为 '_'，
// 使复合 ID 始终满足领域层的 Finding ID 模式。
func sanitizeIDComponent(value string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r == '_' || r == '.' || r == ':' || r == '-':
			return r
		default:
			return '_'
		}
	}, value)
}

func truncateString(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max]
}

func truncateRunes(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max])
}

func inlineEligible(evidence []domain.Evidence) bool {
	for i := range evidence {
		if evidence[i].Side == domain.SideRight &&
			evidence[i].StartLine >= 1 && evidence[i].EndLine >= 1 {
			return true
		}
	}
	return false
}

func recommendationFor(first domain.Evidence) string {
	if first.Side == domain.SideLeft || first.Side == domain.SideRight {
		return fmt.Sprintf("复核 %s 第 %d 行附近的变更，结合上下文确认是否需要补充验证或修复；"+
			"确认属误报的形态可在后续评测调优阶段于策略层抑制。", first.File, first.StartLine)
	}
	return fmt.Sprintf("复核 %s 的整体变更（文件级证据），结合上下文确认是否需要补充验证或修复；"+
		"确认属误报的形态可在后续评测调优阶段于策略层抑制。", first.File)
}

func categoryNoun(category domain.RiskCategory) string {
	switch category {
	case domain.CategorySecurity:
		return "安全"
	case domain.CategoryData:
		return "数据兼容"
	case domain.CategoryAPI:
		return "API 兼容"
	case domain.CategoryReliability:
		return "可靠性"
	case domain.CategoryConcurrency:
		return "并发"
	case domain.CategoryPerformance:
		return "性能"
	case domain.CategoryDelivery:
		return "交付流程"
	case domain.CategorySupplyChain:
		return "供应链"
	case domain.CategoryTestability:
		return "可测试性"
	default:
		return "相关"
	}
}

// buildDimensions 对每个涉及类别套用总分公式并封顶到 [0, 100]；
// 没有信号的维度按 spec/04 第 3 节省略。
func buildDimensions(contributions map[domain.RiskCategory]float64, counts map[domain.RiskCategory]int) []domain.RiskDimension {
	dimensions := make([]domain.RiskDimension, 0, len(contributions))
	for category, raw := range contributions {
		score := clampScore(int(math.Round(raw)))
		count := counts[category]
		dimensions = append(dimensions, domain.RiskDimension{
			Category:    category,
			Score:       score,
			Level:       domain.LevelFromScore(score),
			SignalCount: count,
			Summary: fmt.Sprintf("共有 %d 条确定性线索指向该维度，维度得分 %d 分；"+
				"所有发现均为待人工确认的候选结论，需结合上下文复核。", count, score),
		})
	}
	return dimensions
}

func clampScore(score int) int {
	if score < 0 {
		return 0
	}
	if score > 100 {
		return 100
	}
	return score
}
