// 命令 go-risk-analyzer 是 change-risk-analyzer 的离线命令行入口。
// 本文件只做进程装配，不含任何业务逻辑：参数解析与完整编排都在同包
// app.go 中实现，以便通过 Go 测试直接调用而无需启动子进程。
package main

import (
	"context"
	"os"
)

func main() {
	os.Exit(Run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}
