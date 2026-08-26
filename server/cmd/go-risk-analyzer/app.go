// app.go 实现 go-risk-analyzer 的参数解析与离线分析编排：把既有的事件
// 解析、unified diff 解析、确定性信号运行器、策略引擎和报告构建渲染串成
// 一条完全无网络的链路，并把通过双重校验的报告写入输出目录。
//
// 行为约定（DEVELOPMENT_PLAN 第 7 节任务表切片 13）：
//   - 分析成功即退出码 0：即使存在高风险发现也不阻塞合并；可配置门禁属
//     Phase 4 调优切片（spec/05-github-action-contract.md 第 9 节「MVP
//     始终以零退出码生成报告，除非输入或程序本身无法安全运行」）；
//   - 操作类失败（文件不存在、事件/diff 非法、报告构建校验失败、写盘
//     失败）返回非零退出码，并向 stderr 输出一条简明中文错误；错误信息
//     只包含用户提供的路径与上游包的结构化原因，绝不包含原始文件内容、
//     patch 内容或任何 Secret；
//   - 本命令不发起任何网络请求，也不执行输入内容中的代码。
//
// 编排逻辑集中在可被 Go 测试直接调用的函数中，main.go 只是进程装配薄壳。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"change-risk-analyzer/internal/change"
	"change-risk-analyzer/internal/domain"
	"change-risk-analyzer/internal/event"
	"change-risk-analyzer/internal/policy"
	"change-risk-analyzer/internal/report"
	"change-risk-analyzer/internal/signals"
)

// AnalyzerVersion 同时用于 --version 输出与报告 analyzer_version 字段。
// 离线内核阶段带 dev 后缀；版本化二进制发布属后续切片（任务表切片 16）。
const AnalyzerVersion = "0.1.0-dev"

// 报告输出文件名与 spec/05-github-action-contract.md 第 6 节 Artifact 约定一致。
const (
	reportJSONFileName = "risk-report.json"
	reportMDFileName   = "risk-report.md"
)

// 进程退出码：0 分析成功；1 操作失败；2 参数或用法错误。
const (
	exitSuccess = iota
	exitFailure
	exitUsage
)

const usageText = `go-risk-analyzer：Pull Request 变更风险离线分析命令行。

用法：
  go-risk-analyzer analyze --event <事件JSON> --diff <diff文件> --output <输出目录>
  go-risk-analyzer --version

子命令 analyze 在完全无网络的条件下完成一次风险分析，并在输出目录写出
risk-report.json 与 risk-report.md 两份报告。分析成功即以退出码 0 结束，
即使存在高风险发现也不阻塞合并；只有输入非法或无法写盘时才返回非零退出码。

参数：
  --event    GitHub pull_request 事件 JSON 文件路径（必填）。
  --diff     git unified diff 文件路径（必填），空 diff 表示没有文件变化。
  --output   报告输出目录（必填），不存在时会自动创建。
  --version  打印版本号后退出。

退出码：0 分析成功；1 操作失败；2 参数或用法错误。
`

// options 保存 analyze 子命令解析后的全部输入。stdout 由 Run 注入，
// 用于成功提示；测试可以直接构造该结构调用 runAnalyze。
type options struct {
	eventPath   string
	diffPath    string
	outputDir   string
	showVersion bool
	stdout      io.Writer
}

// Run 是进程入口适配层：分发子命令并把结果映射为退出码。
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usageText)
		return exitUsage
	}
	switch args[0] {
	case "analyze":
		return runAnalyzeCommand(ctx, args[1:], stdout, stderr)
	case "--version", "version":
		printVersion(stdout)
		return exitSuccess
	case "--help", "-h", "help":
		fmt.Fprint(stdout, usageText)
		return exitSuccess
	default:
		fmt.Fprintf(stderr, "go-risk-analyzer：无法识别的命令或选项 %q\n\n%s", args[0], usageText)
		return exitUsage
	}
}

// runAnalyzeCommand 解析 analyze 子命令参数并执行编排，映射退出码。
func runAnalyzeCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	opts, err := parseAnalyzeArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "go-risk-analyzer：%v\n\n%s", err, usageText)
		return exitUsage
	}
	if opts.showVersion {
		printVersion(stdout)
		return exitSuccess
	}
	opts.stdout = stdout
	if err := runAnalyze(ctx, opts); err != nil {
		fmt.Fprintf(stderr, "go-risk-analyzer：%v\n", err)
		return exitFailure
	}
	return exitSuccess
}

func printVersion(w io.Writer) {
	fmt.Fprintf(w, "go-risk-analyzer version %s\n", AnalyzerVersion)
}

// parseAnalyzeArgs 解析 analyze 子命令的旗标并校验必填项。
// flag 包自带的英文错误输出被屏蔽，统一由调用方输出中文提示。
func parseAnalyzeArgs(args []string) (*options, error) {
	opts := &options{}
	fs := flag.NewFlagSet("analyze", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.eventPath, "event", "", "GitHub pull_request 事件 JSON 文件路径（必填）")
	fs.StringVar(&opts.diffPath, "diff", "", "unified diff 文件路径（必填）")
	fs.StringVar(&opts.outputDir, "output", "", "报告输出目录（必填）")
	fs.BoolVar(&opts.showVersion, "version", false, "打印版本号后退出")
	if err := fs.Parse(args); err != nil {
		return nil, fmt.Errorf("参数解析失败：%w", err)
	}
	if opts.showVersion {
		// --version 优先于必填校验，便于脚本探测版本。
		return opts, nil
	}
	if fs.NArg() > 0 {
		return nil, fmt.Errorf("存在未识别的参数 %q", fs.Arg(0))
	}
	switch {
	case opts.eventPath == "":
		return nil, errors.New("缺少必填参数 --event（GitHub pull_request 事件 JSON 文件）")
	case opts.diffPath == "":
		return nil, errors.New("缺少必填参数 --diff（unified diff 文件）")
	case opts.outputDir == "":
		return nil, errors.New("缺少必填参数 --output（报告输出目录）")
	}
	return opts, nil
}

// runAnalyze 执行完整的离线分析链路：
//
//	读事件 → ParsePullRequestEvent → 读 diff → ParseUnifiedDiff（携带事件
//	中的 base/head SHA）→ signals.DefaultRunner().Run → policy.Evaluate →
//	report.Build（领域 + schema 双重校验）→ 渲染并写出两份报告。
//
// 任一步失败都返回错误，由调用方统一输出；成功时向 stdout 写一条不含
// 敏感内容的完成摘要。
func runAnalyze(ctx context.Context, opts *options) error {
	started := time.Now()

	request, err := loadEvent(opts.eventPath)
	if err != nil {
		return err
	}
	parsed, err := loadDiff(opts.diffPath, request)
	if err != nil {
		return err
	}

	changeSignals, err := signals.DefaultRunner().Run(ctx, parsed.ChangeSet)
	if err != nil {
		return fmt.Errorf("确定性信号分析失败：%w", err)
	}
	result, err := policy.Evaluate(changeSignals)
	if err != nil {
		return fmt.Errorf("风险策略求值失败：%w", err)
	}

	built, err := report.Build(report.BuilderInput{
		Request:         request,
		Summary:         buildChangeSummary(parsed.ChangeSet),
		Findings:        result.Findings,
		Dimensions:      result.Dimensions,
		OverallScore:    result.OverallScore,
		OverallLevel:    result.OverallLevel,
		AnalyzerVersion: AnalyzerVersion,
		GeneratedAt:     time.Now().UTC(),
		// 离线模式暂无模型或网络降级源；findings 截断原因由构建器按需追加。
		DegradationReasons: []domain.DegradationReason{},
		Runtime:            buildRuntimeMetadata(parsed.ChangeSet, started),
	})
	if err != nil {
		return fmt.Errorf("报告构建失败：%w", err)
	}

	jsonPath, mdPath, err := writeReports(opts.outputDir, built)
	if err != nil {
		return err
	}
	if opts.stdout != nil {
		fmt.Fprintf(opts.stdout,
			"分析完成：总体 %d 分（%s），共 %d 条发现，状态 %s。\n已写入：%s\n已写入：%s\n",
			built.OverallScore, built.OverallLevel, len(built.Findings), built.Status, jsonPath, mdPath)
	}
	return nil
}

// loadEvent 读取并解析 GitHub pull_request 事件 JSON。
// 错误只携带路径与结构化原因，不回显原始 payload 内容。
func loadEvent(path string) (domain.ReviewRequest, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return domain.ReviewRequest{}, fmt.Errorf("读取事件文件失败：%w", err)
	}
	request, err := event.ParsePullRequestEvent(payload)
	if err != nil {
		return domain.ReviewRequest{}, fmt.Errorf("解析事件文件失败：%w", err)
	}
	return request, nil
}

// loadDiff 读取 unified diff 并以事件中的 SHA 作为变更集合身份。
func loadDiff(path string, request domain.ReviewRequest) (change.ParseResult, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return change.ParseResult{}, fmt.Errorf("读取 diff 文件失败：%w", err)
	}
	parsed, err := change.ParseUnifiedDiff(string(content), change.Options{
		BaseSHA: request.BaseSHA,
		HeadSHA: request.HeadSHA,
	})
	if err != nil {
		return change.ParseResult{}, fmt.Errorf("解析 diff 文件失败：%w", err)
	}
	return parsed, nil
}

// buildChangeSummary 从 ChangeSet 汇总报告所需的变更统计。
// 离线模式下全部文件均被分析，files_seen 与 files_analyzed 相等。
func buildChangeSummary(changeSet domain.ChangeSet) domain.ChangeSummary {
	return domain.ChangeSummary{
		FilesSeen:         changeSet.TotalFiles,
		FilesAnalyzed:     changeSet.TotalFiles,
		Additions:         changeSet.TotalAdditions,
		Deletions:         changeSet.TotalDeletions,
		Truncated:         changeSet.Truncated,
		TruncationReasons: append([]string(nil), changeSet.TruncationReasons...),
	}
}

// buildRuntimeMetadata 汇总运行元数据；耗时来自本进程时钟，
// patch 字节数为存储截断后的实际字节数。
func buildRuntimeMetadata(changeSet domain.ChangeSet, started time.Time) domain.RuntimeMetadata {
	return domain.RuntimeMetadata{
		DurationMs:       int(time.Since(started).Milliseconds()),
		FilesSeen:        changeSet.TotalFiles,
		FilesAnalyzed:    changeSet.TotalFiles,
		PatchBytesSeen:   patchBytesSeen(changeSet),
		ContextTruncated: false,
	}
}

func patchBytesSeen(changeSet domain.ChangeSet) int {
	total := 0
	for i := range changeSet.Files {
		if changeSet.Files[i].Patch != nil {
			total += len(*changeSet.Files[i].Patch)
		}
	}
	return total
}

// writeReports 渲染并写出 risk-report.json 与 risk-report.md。
// 先完成两种渲染再落盘，避免半成品输出；输出目录不存在时自动创建。
func writeReports(outputDir string, built *domain.RiskReport) (string, string, error) {
	jsonData, err := report.RenderJSON(built)
	if err != nil {
		return "", "", fmt.Errorf("渲染报告 JSON 失败：%w", err)
	}
	markdown, err := report.RenderMarkdown(built)
	if err != nil {
		return "", "", fmt.Errorf("渲染报告 Markdown 失败：%w", err)
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return "", "", fmt.Errorf("创建输出目录失败：%w", err)
	}
	jsonPath := filepath.Join(outputDir, reportJSONFileName)
	if err := os.WriteFile(jsonPath, jsonData, 0o644); err != nil {
		return "", "", fmt.Errorf("写入 %s 失败：%w", reportJSONFileName, err)
	}
	mdPath := filepath.Join(outputDir, reportMDFileName)
	if err := os.WriteFile(mdPath, []byte(markdown), 0o644); err != nil {
		return "", "", fmt.Errorf("写入 %s 失败：%w", reportMDFileName, err)
	}
	return jsonPath, mdPath, nil
}
