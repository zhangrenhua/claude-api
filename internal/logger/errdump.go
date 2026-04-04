package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	errDumpDir     = "logs/error_dumps"
	maxErrDumpFiles = 20
)

// DumpInvalidRequest 保存 INVALID_REQUEST 错误的完整诊断信息
// clientBody: 客户端原始请求体
// convertedBody: 转换后发送给上游的请求体
// upstreamResp: 上游返回的响应体
func DumpInvalidRequest(clientBody, convertedBody, upstreamResp string) {
	go doDump(clientBody, convertedBody, upstreamResp)
}

func doDump(clientBody, convertedBody, upstreamResp string) {
	if err := os.MkdirAll(errDumpDir, 0755); err != nil {
		Error("[错误转储] 创建目录失败: %v", err)
		return
	}

	// 淘汰旧文件，保持最多 maxErrDumpFiles-1 个（给新文件留位置）
	rotateErrDumps()

	ts := time.Now().Format("20060102_150405.000")
	filename := fmt.Sprintf("invalid_request_%s.log", ts)
	filePath := filepath.Join(errDumpDir, filename)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("=== INVALID_REQUEST 错误转储 ===\n"))
	sb.WriteString(fmt.Sprintf("时间: %s\n\n", time.Now().Format("2006-01-02 15:04:05.000")))

	sb.WriteString("========== 1. 客户端原始请求 ==========\n")
	sb.WriteString(clientBody)
	sb.WriteString("\n\n")

	sb.WriteString("========== 2. 转换后的请求（发送给上游）==========\n")
	sb.WriteString(convertedBody)
	sb.WriteString("\n\n")

	sb.WriteString("========== 3. 上游返回内容 ==========\n")
	sb.WriteString(upstreamResp)
	sb.WriteString("\n")

	if err := os.WriteFile(filePath, []byte(sb.String()), 0644); err != nil {
		Error("[错误转储] 写入文件失败: %v", err)
		return
	}

	Info("[错误转储] 已保存 INVALID_REQUEST 诊断信息: %s", filename)
}

// rotateErrDumps 淘汰旧文件，保留最近的 maxErrDumpFiles-1 个
func rotateErrDumps() {
	entries, err := os.ReadDir(errDumpDir)
	if err != nil {
		return
	}

	// 只处理 invalid_request_ 开头的 .log 文件
	var files []os.DirEntry
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "invalid_request_") && strings.HasSuffix(e.Name(), ".log") {
			files = append(files, e)
		}
	}

	if len(files) < maxErrDumpFiles {
		return
	}

	// 按文件名排序（文件名包含时间戳，字典序即时间序）
	sort.Slice(files, func(i, j int) bool {
		return files[i].Name() < files[j].Name()
	})

	// 删除最早的文件，保留 maxErrDumpFiles-1 个
	toDelete := len(files) - (maxErrDumpFiles - 1)
	for i := 0; i < toDelete; i++ {
		path := filepath.Join(errDumpDir, files[i].Name())
		if err := os.Remove(path); err != nil {
			Warn("[错误转储] 删除旧文件失败: %s, %v", files[i].Name(), err)
		} else {
			Debug("[错误转储] 淘汰旧文件: %s", files[i].Name())
		}
	}
}
