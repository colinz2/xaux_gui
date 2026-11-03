package gui

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const (
	modelConfigFileName = "model_config.json"
	defaultModelBaseURL = "https://ark.cn-beijing.volces.com/api/coding/v3"
	defaultModelName    = "doubao-seed-2.1-pro"
)

type modelConfig struct {
	BaseURL      string `json:"base_url"`
	APIKey       string `json:"api_key"`
	Model        string `json:"model"`
	SystemPrompt string `json:"system_prompt,omitempty"`
}

func loadModelConfig(rootDir string) (modelConfig, error) {
	data, err := os.ReadFile(filepath.Join(rootDir, modelConfigFileName))
	if errors.Is(err, os.ErrNotExist) {
		return modelConfig{
			BaseURL: defaultModelBaseURL,
			Model:   defaultModelName,
		}, nil
	}
	if err != nil {
		return modelConfig{}, fmt.Errorf("read model configuration: %w", err)
	}
	var config modelConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return modelConfig{}, fmt.Errorf("decode model configuration: %w", err)
	}
	if strings.TrimSpace(config.BaseURL) == "" {
		config.BaseURL = defaultModelBaseURL
	}
	if strings.TrimSpace(config.Model) == "" {
		config.Model = defaultModelName
	}
	return config, nil
}

func loadModelConfigWithMigration(configDir, legacyDir string) (modelConfig, error) {
	config, err := loadModelConfig(configDir)
	if err != nil {
		return modelConfig{}, err
	}
	configPath := filepath.Join(configDir, modelConfigFileName)
	if _, err := os.Stat(configPath); err == nil {
		return config, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return modelConfig{}, fmt.Errorf("check model configuration: %w", err)
	}

	legacyPath := filepath.Join(legacyDir, modelConfigFileName)
	if _, err := os.Stat(legacyPath); err == nil {
		legacyConfig, err := loadModelConfig(legacyDir)
		if err != nil {
			return modelConfig{}, err
		}
		if err := saveModelConfig(configDir, legacyConfig); err != nil {
			return modelConfig{}, fmt.Errorf("migrate model configuration: %w", err)
		}
		return legacyConfig, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return modelConfig{}, fmt.Errorf("check legacy model configuration: %w", err)
	}

	return config, nil
}

func saveModelConfig(rootDir string, config modelConfig) error {
	if strings.TrimSpace(rootDir) == "" {
		return errors.New("model configuration directory is empty")
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("encode model configuration: %w", err)
	}
	data = append(data, '\n')
	path := filepath.Join(rootDir, modelConfigFileName)
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		return fmt.Errorf("create model configuration directory: %w", err)
	}
	temporary, err := os.CreateTemp(rootDir, ".model_config-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary model configuration: %w", err)
	}
	defer os.Remove(temporary.Name())
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write model configuration: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close model configuration: %w", err)
	}
	if err := os.Rename(temporary.Name(), path); err != nil {
		return fmt.Errorf("replace model configuration: %w", err)
	}
	return nil
}

func (c modelConfig) validate() error {
	if strings.TrimSpace(c.APIKey) == "" {
		return localizedError("请填写 API Key", "API key is required", nil)
	}
	if strings.TrimSpace(c.Model) == "" {
		return localizedError("请填写默认模型", "default model is required", nil)
	}
	parsed, err := url.Parse(strings.TrimSpace(c.BaseURL))
	if err != nil || parsed.Host == "" {
		return localizedError("模型 URL 必须是有效的 HTTP 或 HTTPS 地址", "model URL must be a valid HTTP or HTTPS address", nil)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "https" && scheme != "http" {
		return localizedError("模型 URL 必须是有效的 HTTP 或 HTTPS 地址", "model URL must be a valid HTTP or HTTPS address", nil)
	}
	if parsed.User != nil {
		return localizedError("模型 URL 不能包含用户名或密码", "model URL must not include credentials", nil)
	}
	if scheme == "http" && !isLoopbackHost(parsed.Hostname()) {
		return localizedError("远程模型服务必须使用 HTTPS；HTTP 仅允许用于本机回环地址", "remote model services require HTTPS; HTTP is only allowed for loopback addresses", nil)
	}
	if strings.EqualFold(parsed.Hostname(), "ark.cn-beijing.volces.com") &&
		strings.TrimRight(parsed.Path, "/") == "/api/v3" {
		return localizedError(
			fmt.Sprintf("火山引擎 Coding Plan 请使用 %s；/api/v3 不会使用套餐额度，可能产生额外费用", defaultModelBaseURL),
			fmt.Sprintf("Volcengine Coding Plan requires %s; /api/v3 may incur additional charges", defaultModelBaseURL),
			nil,
		)
	}
	return nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") || strings.EqualFold(host, "localhost.") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
