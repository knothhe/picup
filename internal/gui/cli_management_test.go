package gui

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/config"
	"github.com/knothhe/picup/internal/install"
)

func cliTask(t *testing.T, tasks chan func()) {
	t.Helper()
	select {
	case fn := <-tasks:
		fn()
	case <-time.After(3 * time.Second):
		t.Fatal("CLI worker did not dispatch completion")
	}
}

func cliManagementFixture() (*application, chan func()) {
	tasks := make(chan func(), 32)
	a := &application{installDir: "/isolated/bin", config: config.Default()}
	a.cliManagement.runtime = &cliManagementRuntime{
		source: func() (string, error) { return "/isolated/bundled-picup", nil },
		inspect: func(context.Context, string) install.CLIInstallation {
			return install.CLIInstallation{State: install.CLIMissing, Path: "/isolated/bin/picup", CanInstall: true}
		},
		install:   func(context.Context, string, string) error { return nil },
		uninstall: func(context.Context, string) error { return nil },
		dispatch:  func(fn func()) { tasks <- fn },
	}
	return a, tasks
}

func TestCLIInitialFrameOnlyQueuesOneBackgroundInspection(t *testing.T) {
	a, tasks := cliManagementFixture()
	var inspections atomic.Int32
	a.cliManagement.runtime.inspect = func(context.Context, string) install.CLIInstallation {
		inspections.Add(1)
		return install.CLIInstallation{State: install.CLIMissing, CanInstall: true}
	}
	for range 5 {
		a.ensureCLIManagement()
	}
	if inspections.Load() != 0 {
		t.Fatal("frame synchronously inspected files")
	}
	cliTask(t, tasks)
	cliTask(t, tasks)
	for range 5 {
		a.ensureCLIManagement()
	}
	if inspections.Load() != 1 || a.cliManagement.phase != "idle" {
		t.Fatal("repeated frame triggered disk inspection", inspections.Load(), a.cliManagement.phase)
	}
	a.refreshCLIManagement()
	cliTask(t, tasks)
	if inspections.Load() != 2 {
		t.Fatal("explicit refresh did not inspect")
	}
}

func TestCLILateDirectoryResultCannotReplaceNewerStatus(t *testing.T) {
	a, tasks := cliManagementFixture()
	oldResult := make(chan struct{})
	a.cliManagement.runtime.inspect = func(_ context.Context, directory string) install.CLIInstallation {
		if directory == "/isolated/bin" {
			<-oldResult
		}
		return install.CLIInstallation{State: install.CLIManaged, Path: directory + "/picup", Version: directory}
	}
	a.refreshCLIManagement()
	a.installDir = "/isolated/new-bin"
	a.refreshCLIManagement()
	cliTask(t, tasks)
	close(oldResult)
	cliTask(t, tasks)
	if a.cliManagement.status.Version != "/isolated/new-bin" {
		t.Fatal("late result replaced current directory status", a.cliManagement.status)
	}
}

func TestCLIUninstallCapturesDirectoryAndPreventsDuplicateAction(t *testing.T) {
	a, tasks := cliManagementFixture()
	started := make(chan string, 2)
	release := make(chan struct{})
	a.cliManagement = cliManagementState{initialized: true, phase: "idle", target: a.installDir, status: install.CLIInstallation{State: install.CLIManaged, CanUninstall: true}, runtime: a.cliManagement.runtime}
	a.cliManagement.runtime.uninstall = func(_ context.Context, directory string) error { started <- directory; <-release; return nil }
	a.manageCLI(true)
	a.manageCLI(true)
	select {
	case directory := <-started:
		if directory != "/isolated/bin" {
			t.Fatal(directory)
		}
	case <-time.After(time.Second):
		t.Fatal("uninstall did not start")
	}
	if !a.busy || !a.cliManagement.operationActive() {
		t.Fatal("operation did not hold lifecycle guard")
	}
	a.installDir = "/isolated/new-bin"
	close(release)
	cliTask(t, tasks)
	if a.busy || a.cliManagement.operationActive() || a.cliManagement.target != "/isolated/bin" {
		t.Fatal("action lost its captured directory or busy recovery")
	}
	select {
	case <-started:
		t.Fatal("duplicate uninstall ran")
	default:
	}
}

func TestCLIFailedUninstallRefreshesActualStateAndAllowsRetry(t *testing.T) {
	a, tasks := cliManagementFixture()
	denied := errors.New("permission fixture")
	a.cliManagement.initialized = true
	a.cliManagement.phase = "idle"
	a.cliManagement.target = a.installDir
	a.cliManagement.status = install.CLIInstallation{State: install.CLIManaged, CanUninstall: true}
	a.cliManagement.runtime.uninstall = func(context.Context, string) error { return denied }
	a.cliManagement.runtime.inspect = func(context.Context, string) install.CLIInstallation {
		return install.CLIInstallation{State: install.CLIManaged, Version: "0.1.1", CanInstall: true, CanUninstall: true}
	}
	for range 2 {
		a.manageCLI(true)
		cliTask(t, tasks)
		if !errors.Is(a.cliManagement.err, denied) || a.busy || a.cliManagement.status.Version != "0.1.1" {
			t.Fatal("failed operation lost actual install or cannot retry")
		}
	}
}

func TestCLIManagementNativeStatesAtMinimumWidthAndEnglish(t *testing.T) {
	defer SetUILanguage("zh-CN")
	for _, language := range []string{"zh-CN", "en"} {
		for _, dark := range []bool{false, true} {
			for _, state := range []install.CLIInstallationState{install.CLIMissing, install.CLIManaged, install.CLIUnmanaged, install.CLIForeign, install.CLIChanged, install.CLIRemovalPending} {
				t.Run(fmt.Sprintf("%s/dark=%t/%s", language, dark, state), func(t *testing.T) {
					SetUILanguage(language)
					a, _ := cliManagementFixture()
					a.tab = 2
					a.settingsTab = 3
					a.cliManagement.initialized = true
					a.cliManagement.phase = "idle"
					a.cliManagement.target = a.installDir
					a.cliManagement.status = install.CLIInstallation{State: state, Path: "/Users/example/a-long-install-directory/command-line-tools/bin/picup", Version: "0.1.1", CanInstall: state == install.CLIMissing || state == install.CLIManaged, CanUninstall: state == install.CLIManaged || state == install.CLIRemovalPending}
					tt := ui.NewTester(nativeTestView(a), 760, 620)
					tt.SetDark(dark)
					tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1.2})
					tt.Frame()
					if !tt.HasText(tr(cliStatusLabel(state))) || !tt.HasText(tr("安装 / 更新 CLI")) || !tt.HasText(tr("卸载 CLI")) {
						t.Fatal("missing state or management actions", tt.Texts())
					}
					for _, label := range []string{"安装 / 更新 CLI", "刷新安装情况", "卸载 CLI"} {
						if rectangle, ok := tt.Find(tr(label)); ok && (rectangle.X < 0 || rectangle.X+rectangle.W > 760) {
							t.Fatalf("action overflows minimum window: %s %v", label, rectangle)
						}
					}
				})
			}
		}
	}
}
