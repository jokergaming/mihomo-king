package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"mihomo-king/internal/config"
	"mihomo-king/internal/core"
	"mihomo-king/internal/subscription"
)

func switchLanguage(t *testing.T, m Model) Model {
	t.Helper()
	m, cmd := press(m, tea.KeyMsg{Type: tea.KeyCtrlL})
	if cmd == nil || !m.languageBusy {
		t.Fatal("global language switch did not start")
	}
	if _, duplicate := press(m, tea.KeyMsg{Type: tea.KeyCtrlL}); duplicate != nil {
		t.Fatal("competing language saves")
	}
	m, _ = press(m, cmd())
	if m.statusErr || m.languageBusy {
		t.Fatalf("language switch failed: %s", m.status)
	}
	return m
}

func hasChinese(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool { return unicode.Is(unicode.Han, r) })
}

func TestLanguageSwitchAcrossScreensAndPersistence(t *testing.T) {
	m := editableSubsTestModel(t)
	m, _ = press(m, tea.WindowSizeMsg{Width: 160, Height: 48})
	m.autostartLoaded = true
	for _, screen := range []screen{screenDashboard, screenSubs, screenNodes, screenSettings} {
		m.screen = screen
		if view := m.View(); hasChinese(view) {
			t.Fatalf("English screen %d contains Chinese:\n%s", screen, view)
		}
	}
	for _, row := range m.settingsList.Items() {
		if entry := row.(item); entry.id == settingAutostart && entry.title != "autostart" {
			t.Fatalf("autostart title = %q", entry.title)
		}
	}
	m = switchLanguage(t, m)
	for _, screen := range []screen{screenDashboard, screenSubs, screenNodes, screenSettings} {
		m.screen = screen
		view := m.View()
		if !strings.Contains(view, "1 主界面") || !strings.Contains(m.help(), "ctrl+l 切换语言") {
			t.Fatalf("Chinese screen %d not translated:\n%s", screen, view)
		}
	}
	for _, row := range m.settingsList.Items() {
		entry := row.(item)
		if !hasChinese(entry.title) || !hasChinese(m.settingPrompt(entry.id)) {
			t.Fatalf("untranslated setting: %#v", entry)
		}
	}
	s, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if s.Language != config.LanguageChinese {
		t.Fatal("new process lost saved language")
	}
	m = switchLanguage(t, m)
	if view := m.View(); hasChinese(view) {
		t.Fatalf("switching back left Chinese:\n%s", view)
	}
}

func TestGlobalLanguageSwitchPreservesFormsAndPrompts(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup func(*Model)
		want  string
	}{
		{"add subscription", func(m *Model) { m.screen, m.addForm = screenSubs, addSub }, "添加订阅"},
		{"import file", func(m *Model) { m.screen, m.addForm = screenSubs, addFile }, "导入订阅文件"},
		{"add node", func(m *Model) { m.screen, m.addForm = screenSubs, addNode }, "添加本地节点"},
		{"edit subscription", func(m *Model) { m.screen, m.editingSub = screenSubs, true }, "编辑订阅"},
		{"edit node", func(m *Model) { m.screen, m.editingSub, m.editLocal = screenSubs, true, true }, "编辑本地节点"},
		{"update prompt", func(m *Model) { m.screen, m.updatePrompt = screenSubs, "provider" }, "立即更新订阅"},
		{"test URL", func(m *Model) { m.screen, m.editingTestURL = screenNodes, true }, "测速地址"},
		{"setting", func(m *Model) { m.screen, m.editingSetting, m.settingKey = screenSettings, true, settingController }, "控制器"},
		{"core password", func(m *Model) { m.screen, m.tunStage = screenSettings, tunAskCoreCaps }, "sudo 密码"},
		{"TUN password", func(m *Model) { m.screen, m.tunStage = screenDashboard, tunAskPassword }, "sudo 密码"},
		{"TUN conflict", func(m *Model) {
			m.screen, m.tunStage, m.tunForeign = screenDashboard, tunAskForeign, []string{"foreign-tun"}
		}, "其他 TUN 设备"},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := editableSubsTestModel(t)
			test.setup(&m)
			m.nameInput.SetValue("name unchanged")
			m.urlInput.SetValue("https://example.com/raw?mode=off")
			m.settingInput.SetValue("127.0.0.1:12345")
			m.testURLInput.SetValue("https://example.com/test")
			m.pwInput.SetValue("private-password")
			m.pwInput.Focus()
			m.settingInput.SetCursor(3)
			cursor := m.settingInput.Position()
			m = switchLanguage(t, m)
			if m.nameInput.Value() != "name unchanged" || m.urlInput.Value() != "https://example.com/raw?mode=off" || m.settingInput.Value() != "127.0.0.1:12345" || m.testURLInput.Value() != "https://example.com/test" || m.pwInput.Value() != "private-password" || !m.pwInput.Focused() || m.settingInput.Position() != cursor {
				t.Fatal("language switch changed form data, cursor or focus")
			}
			if view := m.View(); !strings.Contains(view, test.want) || strings.Contains(view, "private-password") {
				t.Fatalf("prompt incorrect or password exposed:\n%s", view)
			}
		})
	}
}

func TestLanguageSwitchPreservesNodeAndFilterState(t *testing.T) {
	m := editableSubsTestModel(t)
	m.screen, m.groups = screenNodes, testGroups()
	m.curGroup = m.groups[0].Name
	m.showMembers(m.curGroup)
	m.nodes.Select(1)
	selected := m.nodes.SelectedItem().(item).id
	m.chainEntry = "entry unchanged"
	m.nodeDelays = map[string]int{selected: 123}
	m.nodes.SetFilterText(selected)
	m.nodes.SetFilterState(list.Filtering)
	m.nodes.FilterInput.SetCursor(1)
	cursor := m.nodes.FilterInput.Position()
	m = switchLanguage(t, m)
	if m.curGroup != m.groups[0].Name || m.chainEntry != "entry unchanged" || m.nodeDelays[selected] != 123 || m.nodes.FilterValue() != selected || m.nodes.FilterState() != list.Filtering || m.nodes.FilterInput.Position() != cursor || m.nodes.SelectedItem().(item).id != selected {
		t.Fatal("language switch changed current group, chain, delays, selection or filter")
	}
	if m.nodes.FilterInput.Prompt != "筛选：" {
		t.Fatal("filter prompt not translated")
	}
}

func TestLanguageSaveFailureKeepsCurrentLanguage(t *testing.T) {
	m := editableSubsTestModel(t)
	path := filepath.Join(m.settings.AppDir(), "language")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	m, cmd := press(m, tea.KeyMsg{Type: tea.KeyCtrlL})
	m, _ = press(m, cmd())
	if m.settings.Language != config.LanguageEnglish || !m.statusErr || m.languageBusy || !strings.Contains(m.footer(), "save language") {
		t.Fatal("failed save changed language or hid its error")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	m = switchLanguage(t, m)
	if m.settings.Language != config.LanguageChinese {
		t.Fatal("language save could not recover")
	}
}

func TestLanguageSwitchIgnoresStaleFilterResults(t *testing.T) {
	m := editableSubsTestModel(t)
	m.screen = screenSettings
	m.settingsList.SetFilterText("mihomo")
	m.settingsList.SetFilterState(list.Filtering)
	stale := m.settingsList.SetItems(m.settingsList.Items())
	if stale == nil {
		t.Fatal("filter did not return its pending result")
	}
	staleResult := stale()
	m = switchLanguage(t, m)
	m, _ = press(m, staleResult)
	if len(m.settingsList.VisibleItems()) == 0 {
		t.Fatal("stale filter lost current matches")
	}
	for _, row := range m.settingsList.VisibleItems() {
		if !hasChinese(row.(item).title) {
			t.Fatalf("stale filter restored an English row: %#v", row)
		}
	}
	if m.settingsList.FilterValue() != "mihomo" || m.settingsList.FilterState() != list.Filtering {
		t.Fatal("stale filter changed current input")
	}
}

func TestAsyncMessagesUseCurrentLanguageAndPreserveData(t *testing.T) {
	m := editableSubsTestModel(t)
	name := "autostart off / 开机启动"
	note := fetchedNote(message("downloaded %s", name), &subscription.Result{Proxies: 2, Skipped: map[string]int{"ssr": 1}})
	m = switchLanguage(t, m)
	m, _ = press(m, actionMsg{note: note})
	if got := m.render(m.status); got != "已下载 "+name+" · 2 个代理 · 已跳过 1 个链接（ssr）" {
		t.Fatalf("message = %q", got)
	}
	res := &core.Result{Dest: "/tmp/raw-path", Release: core.Release{Tag: "v1", URL: "https://example.com/release"}}
	m, _ = press(m, actionMsg{note: coreNote("mihomo", res), notice: true})
	if view := m.viewSettings(); !strings.Contains(view, "已将 mihomo v1 安装至 /tmp/raw-path") {
		t.Fatalf("core notice not translated: %s", view)
	}
	m = switchLanguage(t, m)
	if view := m.viewSettings(); !strings.Contains(view, "installed mihomo v1 at /tmp/raw-path") {
		t.Fatalf("core notice did not switch back: %s", view)
	}
	m.setError(errors.New("external error: %s / 原始错误"))
	if got := m.render(m.status); got != "external error: %s / 原始错误" {
		t.Fatalf("raw error changed: %q", got)
	}
}
