package gui

import (
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"fyne.io/fyne/v2/widget"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

const (
	localePreference = "ui_language"
	localeChinese    = "zh-CN"
	localeEnglish    = "en"
)

//go:embed translations/*.json
var translationFiles embed.FS

type localeManager struct {
	mu        sync.RWMutex
	bundle    *i18n.Bundle
	current   string
	listeners map[uint64]func()
	nextID    uint64
}

func newLocaleManager() (*localeManager, error) {
	bundle := i18n.NewBundle(language.SimplifiedChinese)
	bundle.RegisterUnmarshalFunc("json", json.Unmarshal)
	for _, name := range []string{"translations/zh-CN.json", "translations/en.json"} {
		data, err := translationFiles.ReadFile(name)
		if err != nil {
			return nil, fmt.Errorf("read translation bundle %q: %w", name, err)
		}
		if _, err := bundle.ParseMessageFileBytes(data, name); err != nil {
			return nil, fmt.Errorf("parse translation bundle %q: %w", name, err)
		}
	}
	return &localeManager{
		bundle:    bundle,
		current:   localeChinese,
		listeners: make(map[uint64]func()),
	}, nil
}

var uiLocale = func() *localeManager {
	manager, err := newLocaleManager()
	if err != nil {
		panic(err)
	}
	return manager
}()

func (m *localeManager) language() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current
}

func (m *localeManager) setLanguage(value string) error {
	if value != localeChinese && value != localeEnglish {
		return fmt.Errorf("unsupported UI language %q", value)
	}
	m.mu.Lock()
	if m.current == value {
		m.mu.Unlock()
		return nil
	}
	m.current = value
	listeners := make([]func(), 0, len(m.listeners))
	for _, listener := range m.listeners {
		listeners = append(listeners, listener)
	}
	m.mu.Unlock()
	for _, listener := range listeners {
		listener()
	}
	return nil
}

func (m *localeManager) subscribe(listener func()) func() {
	m.mu.Lock()
	id := m.nextID
	m.nextID++
	m.listeners[id] = listener
	m.mu.Unlock()
	return func() {
		m.mu.Lock()
		delete(m.listeners, id)
		m.mu.Unlock()
	}
}

func (m *localeManager) translate(key string, data any) string {
	localizer := i18n.NewLocalizer(m.bundle, m.language())
	text, err := localizer.Localize(&i18n.LocalizeConfig{MessageID: key, TemplateData: data})
	if err != nil {
		log.Printf("translate UI message %q: %v", key, err)
		return "[" + key + "]"
	}
	return text
}

func tr(key string) string {
	return uiLocale.translate(key, nil)
}

func trData(key string, data any) string {
	return uiLocale.translate(key, data)
}

type localeBindings struct {
	updates     []func()
	unsubscribe func()
	labels      map[*widget.Label]*localizedText
	buttons     map[*widget.Button]*localizedText
}

type localizedText struct {
	key  string
	data any
}

func (t *localizedText) text() string {
	data := t.data
	if factory, ok := data.(func() any); ok {
		data = factory()
	}
	return uiLocale.translate(t.key, data)
}

func (t *localizedText) String() string {
	return t.text()
}

func (b *localeBindings) setLabel(label *widget.Label, key string, data any) {
	if b == nil {
		label.SetText((&localizedText{key: key, data: data}).text())
		return
	}
	if b.labels == nil {
		b.labels = make(map[*widget.Label]*localizedText)
	}
	text := b.labels[label]
	if text == nil {
		text = &localizedText{}
		b.labels[label] = text
		b.add(func() {
			if text.key != "" {
				label.SetText(text.text())
			}
		})
	}
	text.key, text.data = key, data
	label.SetText(text.text())
}

func (b *localeBindings) setButton(button *widget.Button, key string, data any) {
	if b == nil {
		button.SetText((&localizedText{key: key, data: data}).text())
		return
	}
	if b.buttons == nil {
		b.buttons = make(map[*widget.Button]*localizedText)
	}
	text := b.buttons[button]
	if text == nil {
		text = &localizedText{}
		b.buttons[button] = text
		b.add(func() {
			if text.key != "" {
				button.SetText(text.text())
			}
		})
	}
	text.key, text.data = key, data
	button.SetText(text.text())
}

func (b *localeBindings) clearLabel(label *widget.Label) {
	b.forgetLabel(label)
	label.SetText("")
}

func (b *localeBindings) forgetLabel(label *widget.Label) {
	if b != nil {
		if text := b.labels[label]; text != nil {
			text.key = ""
			text.data = nil
		}
	}
}

func (b *localeBindings) add(update func()) {
	update()
	if b != nil {
		b.updates = append(b.updates, update)
	}
}

func (b *localeBindings) start() {
	if b == nil || b.unsubscribe != nil {
		return
	}
	b.unsubscribe = uiLocale.subscribe(func() {
		for _, update := range b.updates {
			update()
		}
	})
}

func (b *localeBindings) stop() {
	if b != nil && b.unsubscribe != nil {
		b.unsubscribe()
		b.unsubscribe = nil
	}
}
