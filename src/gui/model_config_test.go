package gui

import (
	"os"
	"path/filepath"
	"testing"

	fynetest "fyne.io/fyne/v2/test"
)

func TestModelConfigRoundTrip(t *testing.T) {
	root := t.TempDir()
	want := modelConfig{
		BaseURL:      "https://example.test/v1",
		APIKey:       "test-secret",
		Model:        "test-model",
		SystemPrompt: "请回答得简洁一些。",
	}
	if err := saveModelConfig(root, want); err != nil {
		t.Fatal(err)
	}
	want.Model = "replacement-model"
	if err := saveModelConfig(root, want); err != nil {
		t.Fatal(err)
	}
	got, err := loadModelConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("loaded configuration = %+v, want %+v", got, want)
	}
	if _, err := os.Stat(filepath.Join(root, modelConfigFileName)); err != nil {
		t.Fatal(err)
	}
}

func TestModelConfigMigratesFromSessionRoot(t *testing.T) {
	configDir := filepath.Join(t.TempDir(), "app-config")
	legacyDir := filepath.Join(t.TempDir(), "sessions")
	want := modelConfig{
		BaseURL: "https://example.test/v1",
		APIKey:  "test-secret",
		Model:   "test-model",
	}
	if err := saveModelConfig(legacyDir, want); err != nil {
		t.Fatal(err)
	}

	got, err := loadModelConfigWithMigration(configDir, legacyDir)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("migrated configuration = %+v, want %+v", got, want)
	}
	if _, err := os.Stat(filepath.Join(configDir, modelConfigFileName)); err != nil {
		t.Fatalf("configuration was not copied to the application directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(legacyDir, modelConfigFileName)); err != nil {
		t.Fatalf("legacy configuration should be preserved: %v", err)
	}
}

func TestModelConfigInApplicationRootTakesPrecedence(t *testing.T) {
	configDir := filepath.Join(t.TempDir(), "app-config")
	legacyDir := filepath.Join(t.TempDir(), "sessions")
	want := modelConfig{
		BaseURL: "https://example.test/v1",
		APIKey:  "new-secret",
		Model:   "new-model",
	}
	if err := saveModelConfig(configDir, want); err != nil {
		t.Fatal(err)
	}
	if err := saveModelConfig(legacyDir, modelConfig{
		BaseURL: "https://legacy.example/v1",
		APIKey:  "legacy-secret",
		Model:   "legacy-model",
	}); err != nil {
		t.Fatal(err)
	}

	got, err := loadModelConfigWithMigration(configDir, legacyDir)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("loaded configuration = %+v, want application config %+v", got, want)
	}
}

func TestAppSettingReloadModelConfigReadsApplicationRoot(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()

	configDir := app.Storage().RootURI().Path()
	want := modelConfig{
		BaseURL: "https://example.test/v1",
		APIKey:  "test-secret",
		Model:   "test-model",
	}
	if err := saveModelConfig(configDir, want); err != nil {
		t.Fatal(err)
	}

	setting := &appSetting{Setting: Setting{sessionRootDir: t.TempDir()}}
	if err := setting.reloadModelConfig(); err != nil {
		t.Fatal(err)
	}
	if setting.modelConfig != want {
		t.Fatalf("loaded configuration = %+v, want %+v", setting.modelConfig, want)
	}
	if setting.modelConfigDir != configDir {
		t.Fatalf("configuration directory = %q, want %q", setting.modelConfigDir, configDir)
	}
}

func TestModelConfigDefaultsToVolcengineCodingPlan(t *testing.T) {
	config, err := loadModelConfig(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if config.BaseURL != defaultModelBaseURL || config.Model != defaultModelName {
		t.Fatalf("default model config = %+v", config)
	}
}

func TestModelConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  modelConfig
		wantErr bool
	}{
		{
			name:    "missing key",
			config:  modelConfig{BaseURL: "https://example.test/v1", Model: "model"},
			wantErr: true,
		},
		{
			name:    "missing model",
			config:  modelConfig{BaseURL: "https://example.test/v1", APIKey: "key"},
			wantErr: true,
		},
		{
			name:    "invalid URL",
			config:  modelConfig{BaseURL: "file:///tmp/model", APIKey: "key", Model: "model"},
			wantErr: true,
		},
		{
			name:    "rejects remote HTTP to protect API key",
			config:  modelConfig{BaseURL: "http://example.test/v1", APIKey: "key", Model: "model"},
			wantErr: true,
		},
		{
			name:   "allows local HTTP-compatible service",
			config: modelConfig{BaseURL: "http://127.0.0.1:8080/v1", APIKey: "key", Model: "model"},
		},
		{
			name:    "rejects credentials embedded in URL",
			config:  modelConfig{BaseURL: "https://user:pass@example.test/v1", APIKey: "key", Model: "model"},
			wantErr: true,
		},
		{
			name:   "valid compatible endpoint",
			config: modelConfig{BaseURL: "https://example.test/v1", APIKey: "key", Model: "model"},
		},
		{
			name: "rejects billable non-plan endpoint",
			config: modelConfig{
				BaseURL: "https://ark.cn-beijing.volces.com/api/v3",
				APIKey:  "key",
				Model:   "doubao-seed-2.1-pro",
			},
			wantErr: true,
		},
		{
			name: "accepts Coding Plan endpoint",
			config: modelConfig{
				BaseURL: defaultModelBaseURL,
				APIKey:  "key",
				Model:   "doubao-seed-2.1-pro",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.config.validate()
			if (err != nil) != test.wantErr {
				t.Fatalf("validate() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}
