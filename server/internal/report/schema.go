// Package report 负责报告的构建和校验。
// 校验分为两层：领域不变式校验（internal/domain）和 JSON Schema 协议校验（本包）。
// 渲染器（JSON/Markdown/Step Summary）不改变报告语义。
package report

import (
	_ "embed"
	"encoding/json"

	"github.com/xeipuuv/gojsonschema"
)

// SchemaVersion 是当前支持的协议版本，与 risk-report.schema.json 的 const 一致。
const SchemaVersion = "risk-report/v1"

//go:embed schemas/risk-report.schema.json
var schemaJSON []byte

// SchemaLoader 返回内置协议的 Schema loader。
// 副本来源为 spec/schemas/risk-report.schema.json，发布前需验证两者一致。
func SchemaLoader() gojsonschema.JSONLoader {
	return gojsonschema.NewStringLoader(string(schemaJSON))
}

// ValidateAgainstSchema 校验序列化后的报告 JSON 是否符合 risk-report.schema.json。
// 返回 (通过, 错误描述列表, 致命错误)。任何失败都应视为协议违规。
func ValidateAgainstSchema(reportJSON []byte) (valid bool, errors []string, err error) {
	if !json.Valid(reportJSON) {
		return false, nil, &SchemaError{Reason: "report 不是合法 JSON"}
	}
	document := gojsonschema.NewBytesLoader(reportJSON)
	result, err := gojsonschema.Validate(SchemaLoader(), document)
	if err != nil {
		return false, nil, &SchemaError{Reason: "schema 校验执行失败: " + err.Error()}
	}
	if result.Valid() {
		return true, nil, nil
	}
	for _, e := range result.Errors() {
		errors = append(errors, e.String())
	}
	return false, errors, nil
}

// SchemaError 表示 schema 校验本身无法执行（而非报告不合法）。
type SchemaError struct {
	Reason string
}

func (e *SchemaError) Error() string {
	return e.Reason
}
