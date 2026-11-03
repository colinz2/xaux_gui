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
		return "", localizedMessageError("knowledge.folder_required", nil, "interview knowledge folder is required", nil)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return "", localizedMessageError("knowledge.folder_read_failed", nil, "read interview knowledge folder failed", err)
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
			return "", localizedMessageError("knowledge.check_failed", map[string]any{"Name": fmt.Sprintf("%q", entry.Name())}, fmt.Sprintf("check knowledge file %q failed", entry.Name()), err)
		}
		if !info.Mode().IsRegular() {
			return "", localizedMessageError("knowledge.not_file", map[string]any{"Name": fmt.Sprintf("%q", entry.Name())}, fmt.Sprintf("knowledge file %q is not a regular file", entry.Name()), nil)
		}
		count++
		if count > maxKnowledgeFiles {
			return "", localizedMessageError("knowledge.too_many", map[string]any{"Limit": maxKnowledgeFiles}, fmt.Sprintf("knowledge files exceed limit %d", maxKnowledgeFiles), nil)
		}
		text, err := readInterviewReference(filepath.Join(directory, entry.Name()),
			&localizedText{key: "reference.knowledge_file", data: map[string]any{"Name": entry.Name()}})
		if err != nil {
			return "", err
		}
		if text == "" {
			return "", localizedMessageError("knowledge.empty_file", map[string]any{"Name": fmt.Sprintf("%q", entry.Name())}, fmt.Sprintf("knowledge file %q is empty", entry.Name()), nil)
		}
		separator := ""
		if result.Len() != 0 {
			separator = "\n\n---\n\n"
		}
		if result.Len()+len(separator)+len(text) > maxInterviewContextBytes {
			return "", localizedMessageError("knowledge.too_large", map[string]any{"Limit": maxInterviewContextBytes / 1024}, fmt.Sprintf("knowledge content exceeds %d KB", maxInterviewContextBytes/1024), nil)
		}
		result.WriteString(separator)
		result.WriteString(text)
	}
	if count == 0 {
		return "", localizedMessageError("knowledge.no_files", nil, "knowledge folder has no Markdown or text files", nil)
	}
	return result.String(), nil
}
