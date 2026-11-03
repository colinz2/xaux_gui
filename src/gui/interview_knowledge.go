package gui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const maxKnowledgeFiles = 64

func loadInterviewKnowledge(enabled bool, directory string) (string, error) {
	if !enabled {
		return "", nil
	}
	if strings.TrimSpace(directory) == "" {
		return "", fmt.Errorf("已启用面试知识，请选择文件夹")
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return "", fmt.Errorf("读取面试知识文件夹失败：%w", err)
	}
	var result strings.Builder
	count := 0
	for _, entry := range entries {
		extension := strings.ToLower(filepath.Ext(entry.Name()))
		if entry.IsDir() || (extension != ".md" && extension != ".markdown" && extension != ".txt") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return "", fmt.Errorf("检查知识文件 %q 失败：%w", entry.Name(), err)
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("知识文件 %q 不是普通文件", entry.Name())
		}
		count++
		if count > maxKnowledgeFiles {
			return "", fmt.Errorf("知识文件不能超过 %d 个", maxKnowledgeFiles)
		}
		text, err := readInterviewReference(filepath.Join(directory, entry.Name()), "知识文件 "+entry.Name())
		if err != nil {
			return "", err
		}
		if text == "" {
			return "", fmt.Errorf("知识文件 %q 为空", entry.Name())
		}
		separator := ""
		if result.Len() != 0 {
			separator = "\n\n---\n\n"
		}
		if result.Len()+len(separator)+len(text) > maxInterviewContextBytes {
			return "", fmt.Errorf("面试知识文件总内容不能超过 %d KB", maxInterviewContextBytes/1024)
		}
		result.WriteString(separator)
		result.WriteString(text)
	}
	if count == 0 {
		return "", fmt.Errorf("知识文件夹中没有 Markdown 或文本文件")
	}
	return result.String(), nil
}
