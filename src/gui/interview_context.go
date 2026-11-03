package gui

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const maxInterviewContextBytes = 32 * 1024

type interviewContext struct {
	jobDescription string
	resume         string
	knowledge      string
}

func validateInterviewContext(text, label string) (string, error) {
	text = strings.TrimSpace(text)
	if !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
		return "", fmt.Errorf("%s必须是 UTF-8 文本", label)
	}
	if len(text) > maxInterviewContextBytes {
		return "", fmt.Errorf("%s不能超过 %d KB", label, maxInterviewContextBytes/1024)
	}
	return text, nil
}

func loadInterviewResume(enabled bool, path string) (string, error) {
	if !enabled {
		return "", nil
	}
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("已启用简历，请选择 Markdown 文件")
	}
	if !strings.EqualFold(filepath.Ext(path), ".md") && !strings.EqualFold(filepath.Ext(path), ".markdown") {
		return "", fmt.Errorf("简历文件必须是 Markdown（.md 或 .markdown）")
	}
	resume, err := readInterviewReference(path, "简历文件")
	if err != nil {
		return "", err
	}
	if resume == "" {
		return "", fmt.Errorf("简历文件为空，请填写内容或关闭使用简历")
	}
	return resume, nil
}

func readInterviewReference(path, label string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("打开%s失败：%w", label, err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxInterviewContextBytes+1))
	if err != nil {
		return "", fmt.Errorf("读取%s失败：%w", label, err)
	}
	return validateInterviewContext(string(data), label)
}
