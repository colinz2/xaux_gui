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

func validateInterviewContext(text string, label fmt.Stringer) (string, error) {
	text = strings.TrimSpace(text)
	if !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
		return "", localizedMessageError("reference.utf8", map[string]any{"Label": label}, "reference must be valid UTF-8 text", nil)
	}
	if len(text) > maxInterviewContextBytes {
		return "", localizedMessageError("reference.too_large", map[string]any{"Label": label, "Limit": maxInterviewContextBytes / 1024},
			fmt.Sprintf("reference exceeds %d KB", maxInterviewContextBytes/1024), nil)
	}
	return text, nil
}

func loadInterviewResume(enabled bool, path string) (string, error) {
	if !enabled {
		return "", nil
	}
	if strings.TrimSpace(path) == "" {
		return "", localizedMessageError("reference.resume_required", nil, "resume file is required", nil)
	}
	if !strings.EqualFold(filepath.Ext(path), ".md") && !strings.EqualFold(filepath.Ext(path), ".markdown") {
		return "", localizedMessageError("reference.resume_format", nil, "resume must be a Markdown file", nil)
	}
	resume, err := readInterviewReference(path, &localizedText{key: "reference.resume_file"})
	if err != nil {
		return "", err
	}
	if resume == "" {
		return "", localizedMessageError("reference.resume_empty", nil, "resume file is empty", nil)
	}
	return resume, nil
}

func readInterviewReference(path string, label fmt.Stringer) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", localizedMessageError("reference.open_failed", map[string]any{"Label": label}, "open reference failed", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxInterviewContextBytes+1))
	if err != nil {
		return "", localizedMessageError("reference.read_failed", map[string]any{"Label": label}, "read reference failed", err)
	}
	return validateInterviewContext(string(data), label)
}
