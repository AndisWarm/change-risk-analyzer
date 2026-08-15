package report

import (
	"change-risk-analyzer/internal/domain"
	"encoding/json"
	"fmt"
)

// ValidationResult 汇总领域校验和 schema 校验的结果。
type ValidationResult struct {
	DomainErrors []string // 领域不变式违规
	SchemaErrors []string // JSON Schema 协议违规
	DomainOK     bool
	SchemaOK     bool
}

// Valid 返回两层校验是否都通过。
func (r ValidationResult) Valid() bool {
	return r.DomainOK && r.SchemaOK
}

// Error 返回汇总的错误描述，全部通过时返回 nil。
func (r ValidationResult) Error() error {
	if r.Valid() {
		return nil
	}
	return fmt.Errorf("report validation failed: %d domain problems, %d schema problems", len(r.DomainErrors), len(r.SchemaErrors))
}

// Validate 对报告执行完整校验：
// 1. 领域不变式（domain.RiskReport.Validate，含稳定排序检查由调用方负责）。
// 2. 序列化后 JSON Schema 协议校验。
func Validate(rep *domain.RiskReport) (ValidationResult, error) {
	var res ValidationResult

	if rep == nil {
		return res, fmt.Errorf("report 不能为 nil")
	}

	if err := rep.Validate(); err != nil {
		if ve, ok := err.(*domain.ValidationError); ok {
			res.DomainErrors = ve.Problems
		} else {
			return res, err
		}
	}
	res.DomainOK = len(res.DomainErrors) == 0

	reportJSON, err := json.Marshal(rep)
	if err != nil {
		return res, fmt.Errorf("报告序列化失败: %w", err)
	}
	valid, schemaErrs, err := ValidateAgainstSchema(reportJSON)
	if err != nil {
		return res, err
	}
	res.SchemaOK = valid
	res.SchemaErrors = schemaErrs
	return res, nil
}
