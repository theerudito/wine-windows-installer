package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInstallerCommand(t *testing.T) {
	for _, tt := range []struct {
		name, ext string
		want      []string
	}{
		{"exe", ".exe", []string{"wine", "path.exe"}},
		{"msi", ".msi", []string{"wine", "msiexec", "/i", "path.msi"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "path"+tt.ext)
			if err := os.WriteFile(path, nil, 0600); err != nil {
				t.Fatal(err)
			}
			got, err := installerCommand(path)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(got, "\x00") != strings.Join(append(tt.want[:len(tt.want)-1], path), "\x00") {
				t.Fatalf("command = %#v", got)
			}
		})
	}
}

func TestWinePrefixMigratesLegacyDirectory(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, "programas")
	marker := filepath.Join(legacy, "marker")
	if err := os.MkdirAll(legacy, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, []byte("legacy"), 0644); err != nil {
		t.Fatal(err)
	}
	prefix, err := winePrefixForHome(home)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, "Programs Wine")
	if prefix != want {
		t.Fatalf("prefix = %q, want %q", prefix, want)
	}
	if _, err := os.Stat(filepath.Join(prefix, "marker")); err != nil {
		t.Fatalf("legacy prefix was not migrated: %v", err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("legacy prefix still exists: %v", err)
	}
}

func TestCopyPortableExecutableUsesApplicationDirectoryAndSafeTarget(t *testing.T) {
	home := t.TempDir()
	prefix := filepath.Join(home, "Programs Wine")
	sourceDir := filepath.Join(home, "Downloads", "Portable App")
	source := filepath.Join(sourceDir, "portable.exe")
	if err := os.MkdirAll(filepath.Join(sourceDir, "assets", "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("portable"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "assets", "data.bin"), []byte("asset"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "assets", "nested", "config.ini"), []byte("config"), 0600); err != nil {
		t.Fatal(err)
	}
	target, err := copyPortableExecutable(source, prefix, "../Mi App")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(prefix, "mi-app", "portable.exe")
	if target != want {
		t.Fatalf("target = %q, want %q", target, want)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "portable" {
		t.Fatalf("copied executable = %q, err = %v", data, err)
	}
	for _, relative := range []string{"assets/data.bin", "assets/nested/config.ini"} {
		if _, err := os.Stat(filepath.Join(prefix, "mi-app", relative)); !os.IsNotExist(err) {
			t.Fatalf("unexpected companion file %q: %v", relative, err)
		}
	}
	if original, err := os.ReadFile(source); err != nil || string(original) != "portable" {
		t.Fatalf("source executable changed: %q, err = %v", original, err)
	}
}

func TestCopyPortableExecutableRejectsSymlinkSelectedExecutable(t *testing.T) {
	home := t.TempDir()
	sourceDir := filepath.Join(home, "Downloads", "Portable App")
	prefix := filepath.Join(home, "Programs Wine")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(sourceDir, "portable.exe")
	if err := os.WriteFile(source, []byte("portable"), 0600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(home, "outside.exe")
	if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, source); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	if _, err := copyPortableExecutable(source, prefix, "Portable App"); err == nil {
		t.Fatal("expected symlinked executable to be rejected")
	}
	if _, err := os.Stat(filepath.Join(prefix, "portable-app")); !os.IsNotExist(err) {
		t.Fatalf("partial portable directory remains: %v", err)
	}
}

func TestDiscoverInstalledExecutable(t *testing.T) {
	prefix := t.TempDir()
	root := filepath.Join(prefix, "drive_c", "Program Files", "Acme")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "Acme.exe")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	got, count := discoverInstalledExecutable(prefix, map[string]struct{}{})
	if got != path || count != 1 {
		t.Fatalf("got %q, count %d", got, count)
	}
}

func TestDiscoverInstalledExecutableRejectsAmbiguousResults(t *testing.T) {
	prefix := t.TempDir()
	root := filepath.Join(prefix, "drive_c", "Program Files", "Acme")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"one.exe", "two.exe"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	got, count := discoverInstalledExecutable(prefix, map[string]struct{}{})
	if got != "" || count != 2 {
		t.Fatalf("got %q, count %d", got, count)
	}
}

func TestManagedMSIConfigUsesValidatedInstallDirectoryWhenTargetIsAmbiguous(t *testing.T) {
	home := t.TempDir()
	prefix := filepath.Join(home, "Programs Wine")
	installDir := filepath.Join(prefix, "drive_c", "Program Files", "Acme")
	appDir := filepath.Join(prefix, "acme")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(appConfig{Name: "Acme", Prefix: prefix, Type: "msi", InstallDir: installDir})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "config.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := safeMSIInstallDirectory(prefix, "", installDir); err != nil {
		t.Fatalf("ambiguous MSI install directory rejected: %v", err)
	}
	apps, err := listInstalledApps(prefix)
	if err != nil || len(apps) != 1 || apps[0].ID != "acme" || apps[0].Type != "msi" {
		t.Fatalf("apps = %#v, err = %v", apps, err)
	}
}

func TestWriteAppFilesPersistsManagedConfigWithMenuLauncherOnly(t *testing.T) {
	home := t.TempDir()
	prefix := filepath.Join(home, "Programs Wine")
	target := filepath.Join(prefix, "drive_c", "Program Files", "App", "app.exe")
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, nil, 0600); err != nil {
		t.Fatal(err)
	}
	req := InstallRequest{InstallerPath: filepath.Join(home, "Mi App.exe")}
	if err := os.WriteFile(req.InstallerPath, nil, 0600); err != nil {
		t.Fatal(err)
	}
	_, err := writeAppFiles(home, req, target, prefix)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(prefix, "mi-app", "config.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, "Desktop", "mi-app.desktop")); !os.IsNotExist(err) {
		t.Fatalf("unexpected desktop shortcut: %v", err)
	}
	launcher := filepath.Join(home, ".local", "share", "applications", "mi-app.desktop")
	launcherData, err := os.ReadFile(launcher)
	if err != nil || !strings.Contains(string(launcherData), "Exec=env "+desktopExecArg("WINEPREFIX="+filepath.Dir(filepath.Dir(target)))+" wine "+desktopExecArg(target)) || !strings.Contains(string(launcherData), "Path="+desktopExecArg(filepath.Dir(target))) {
		t.Fatalf("menu launcher = %s, err = %v", launcherData, err)
	}
	config, err := os.ReadFile(filepath.Join(prefix, "mi-app", "config.json"))
	if err != nil || !strings.Contains(string(config), filepath.Base(target)) || !strings.Contains(string(config), filepath.Base(prefix)) {
		t.Fatalf("app config = %s, err = %v", config, err)
	}
}

func TestWriteAppFilesDoesNotCreateLauncherForMSI(t *testing.T) {
	home := t.TempDir()
	prefix := filepath.Join(home, "Programs Wine")
	req := InstallRequest{InstallerPath: filepath.Join(home, "Fallback App.msi")}
	_, err := writeAppFiles(home, req, filepath.Join(prefix, "drive_c", "app.exe"), prefix)
	if err != nil {
		t.Fatal(err)
	}
	for _, shortcut := range []string{filepath.Join(home, "Desktop", "fallback-app.desktop"), filepath.Join(home, ".local", "share", "applications", "fallback-app.desktop")} {
		if _, err := os.Stat(shortcut); !os.IsNotExist(err) {
			t.Fatalf("unexpected shortcut %q: %v", shortcut, err)
		}
	}
}

type fakeProcess struct{}

func (fakeProcess) Wait() error { return nil }

type callbackProcess func() error

func (process callbackProcess) Wait() error { return process() }

func awaitInstallerStatus(t *testing.T, statuses <-chan InstallerStatus, want string) InstallerStatus {
	t.Helper()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for {
		select {
		case status := <-statuses:
			if status.Status == want {
				return status
			}
		case <-deadline.C:
			t.Fatalf("timed out waiting for installer status %q", want)
		}
	}
}

func TestInstallUsesWinePrefixAndMSIArguments(t *testing.T) {
	home := t.TempDir()
	old := userHomeDir
	userHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { userHomeDir = old })
	path := filepath.Join(home, "setup.msi")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	var gotName string
	var gotArgs, gotEnv []string
	statuses := make(chan InstallerStatus, 2)
	app := &App{startEnv: func(_ context.Context, env []string, name string, args ...string) (CommandProcess, error) {
		gotEnv, gotName, gotArgs = env, name, args
		return fakeProcess{}, nil
	}, emit: func(_ context.Context, _ string, values ...interface{}) {
		if len(values) == 1 {
			statuses <- values[0].(InstallerStatus)
		}
	}}
	result := app.Install(InstallRequest{InstallerPath: path})
	if !result.Success {
		t.Fatalf("result = %#v", result)
	}
	if gotName != "wine" || strings.Join(gotArgs, "\x00") != strings.Join([]string{"msiexec", "/i", path}, "\x00") || gotEnv[0] != "WINEPREFIX="+filepath.Join(home, "Programs Wine") {
		t.Fatalf("command=%s %#v env=%#v", gotName, gotArgs, gotEnv)
	}
	failed := awaitInstallerStatus(t, statuses, "failed")
	configPath := filepath.Join(home, "Programs Wine", "setup", "config.json")
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("invalid MSI metadata was written: %v", err)
	}
	if !strings.Contains(failed.Message, "ejecutable instalado válido") {
		t.Fatalf("failed status = %#v", failed)
	}
}

func TestInstallPersistsMSIMetadataWhenExecutableIsDiscovered(t *testing.T) {
	home := t.TempDir()
	old := userHomeDir
	userHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { userHomeDir = old })
	path := filepath.Join(home, "setup.msi")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	prefix := filepath.Join(home, "Programs Wine")
	target := filepath.Join(prefix, "drive_c", "Program Files", "Acme", "Acme.exe")
	statuses := make(chan InstallerStatus, 2)
	app := &App{
		startEnv: func(context.Context, []string, string, ...string) (CommandProcess, error) {
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return nil, err
			}
			if err := os.WriteFile(target, nil, 0600); err != nil {
				return nil, err
			}
			return callbackProcess(func() error { return nil }), nil
		},
		emit: func(_ context.Context, _ string, values ...interface{}) {
			if len(values) == 1 {
				statuses <- values[0].(InstallerStatus)
			}
		},
	}
	result := app.Install(InstallRequest{InstallerPath: path})
	if !result.Success {
		t.Fatalf("result = %#v", result)
	}
	completed := awaitInstallerStatus(t, statuses, "completed")
	if !strings.Contains(completed.Message, "correctamente") {
		t.Fatalf("completed status = %#v", completed)
	}
	configPath := filepath.Join(prefix, "setup", "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("valid MSI metadata was not written: %v", err)
	}
	var config appConfig
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("invalid MSI metadata: %v", err)
	}
	installDir := filepath.Dir(target)
	if config.Type != "msi" || config.Target != target || config.InstallDir != installDir {
		t.Fatalf("MSI metadata = %#v, want type %q, target %q, install directory %q", config, "msi", target, installDir)
	}
}

func TestRunPortableUsesWinePrefixAndSelectedExecutable(t *testing.T) {
	home := t.TempDir()
	old := userHomeDir
	userHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { userHomeDir = old })
	path := filepath.Join(home, "Downloads", "Portable App", "portable.exe")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	var gotName string
	var gotArgs, gotEnv []string
	app := &App{startEnv: func(_ context.Context, env []string, name string, args ...string) (CommandProcess, error) {
		gotEnv, gotName, gotArgs = env, name, args
		return fakeProcess{}, nil
	}, emit: func(context.Context, string, ...interface{}) {}}
	result := app.RunPortable(path)
	if !result.Success {
		t.Fatalf("result = %#v", result)
	}
	wantTarget := filepath.Join(home, "Programs Wine", "portable", "portable.exe")
	if gotName != "wine" || strings.Join(gotArgs, "\x00") != wantTarget || strings.Join(gotEnv, "\x00") != "WINEPREFIX="+filepath.Join(home, "Programs Wine") {
		t.Fatalf("command=%s %#v env=%#v", gotName, gotArgs, gotEnv)
	}
	if _, err := os.Stat(filepath.Join(home, ".local", "share", "applications", "portable.desktop")); err != nil {
		t.Fatalf("portable launcher was not created: %v", err)
	}
}

func TestApplicationNameIsDerivedWithoutExtension(t *testing.T) {
	if got := applicationName(filepath.Join("/tmp", "Mi.App.msi")); got != "Mi.App" {
		t.Fatalf("application name = %q", got)
	}
}

func TestUninstallMSIUsesWinePrefixAndOfficialCommand(t *testing.T) {
	home := t.TempDir()
	old := userHomeDir
	userHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { userHomeDir = old })
	prefix := filepath.Join(home, "Programs Wine")
	appDir := filepath.Join(prefix, "acme")
	installDir := filepath.Join(prefix, "drive_c", "Program Files", "Acme")
	if err := os.MkdirAll(installDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(installDir, "Acme.exe")
	if err := os.WriteFile(target, nil, 0600); err != nil {
		t.Fatal(err)
	}
	config, err := json.Marshal(appConfig{Name: "Acme", Target: target, Prefix: prefix, Type: "msi"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "config.json"), config, 0644); err != nil {
		t.Fatal(err)
	}
	var gotName string
	var gotArgs, gotEnv []string
	app := &App{startEnv: func(_ context.Context, env []string, name string, args ...string) (CommandProcess, error) {
		gotEnv, gotName, gotArgs = env, name, args
		return fakeProcess{}, nil
	}, emit: func(context.Context, string, ...interface{}) {}}
	result := app.Uninstall("acme")
	if !result.Success {
		t.Fatalf("result = %#v", result)
	}
	if gotName != "wine" || strings.Join(gotArgs, "\x00") != "uninstaller" || strings.Join(gotEnv, "\x00") != "WINEPREFIX="+filepath.Join(home, "Programs Wine") {
		t.Fatalf("command=%s %#v env=%#v", gotName, gotArgs, gotEnv)
	}
}

func TestRemoveManagedPortableAppRemovesEntireDirectoryAndPreservesSiblings(t *testing.T) {
	home := t.TempDir()
	prefix := filepath.Join(home, "Programs Wine")
	appDir := filepath.Join(prefix, "portable-app")
	sibling := filepath.Join(prefix, "sibling", "keep.txt")
	if err := os.MkdirAll(filepath.Join(appDir, "nested", "deeper"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(sibling), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sibling, []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(appConfig{Name: "Portable App", Target: filepath.Join(appDir, "app.exe"), Prefix: prefix, Type: "portable"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "config.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(appDir, "app.exe"), filepath.Join(appDir, "nested", "deeper", "data.bin")} {
		if err := os.WriteFile(path, []byte("owned"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := removeManagedApp(home, prefix, "portable-app"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(appDir); !os.IsNotExist(err) {
		t.Fatalf("managed app directory still exists: %v", err)
	}
	if data, err := os.ReadFile(sibling); err != nil || string(data) != "keep" {
		t.Fatalf("sibling was changed: %q, %v", data, err)
	}
}

func TestRemoveManagedMSIAppRemovesInstallDirectoryAndPreservesSharedFiles(t *testing.T) {
	home := t.TempDir()
	prefix := filepath.Join(home, "Programs Wine")
	appDir := filepath.Join(prefix, "acme")
	installDir := filepath.Join(prefix, "drive_c", "Program Files", "Acme")
	shared := filepath.Join(prefix, "drive_c", "Program Files", "Shared", "keep.dll")
	if err := os.MkdirAll(filepath.Join(installDir, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(shared), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shared, []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(installDir, "Acme.exe")
	if err := os.WriteFile(target, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installDir, "nested", "data.dat"), []byte("owned"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(appConfig{Name: "Acme", Target: target, Prefix: prefix, Type: "msi"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "config.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	if err := removeManagedApp(home, prefix, "acme"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(installDir); !os.IsNotExist(err) {
		t.Fatalf("MSI install directory still exists: %v", err)
	}
	if _, err := os.Stat(appDir); !os.IsNotExist(err) {
		t.Fatalf("managed metadata directory still exists: %v", err)
	}
	if data, err := os.ReadFile(shared); err != nil || string(data) != "keep" {
		t.Fatalf("shared file was changed: %q, %v", data, err)
	}
}

func TestRemoveManagedMSIAppRejectsSharedOrTraversalTarget(t *testing.T) {
	home := t.TempDir()
	prefix := filepath.Join(home, "Programs Wine")
	appDir := filepath.Join(prefix, "acme")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{
		filepath.Join(prefix, "drive_c", "Program Files", "Acme", "..", "Shared.exe"),
		filepath.Join(prefix, "drive_c", "Program Files", "Shared.exe"),
	} {
		data, err := json.Marshal(appConfig{Name: "Acme", Target: target, Prefix: prefix, Type: "msi"})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(appDir, "config.json"), data, 0644); err != nil {
			t.Fatal(err)
		}
		if err := removeManagedApp(home, prefix, "acme"); err == nil {
			t.Fatalf("expected unsafe MSI target %q to be rejected", target)
		}
	}
}

func TestListInstalledAppsReadsManagedConfigsOnly(t *testing.T) {
	prefix := filepath.Join(t.TempDir(), "Programs Wine")
	if err := os.MkdirAll(filepath.Join(prefix, "portable-app"), 0755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(appConfig{Name: "Portable App", Target: filepath.Join(prefix, "portable-app", "app.exe"), Prefix: prefix, Type: "portable"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prefix, "portable-app", "config.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(prefix, "unmanaged"), 0755); err != nil {
		t.Fatal(err)
	}
	apps, err := listInstalledApps(prefix)
	if err != nil || len(apps) != 1 || apps[0].ID != "portable-app" || apps[0].Name != "Portable App" {
		t.Fatalf("apps = %#v, err = %v", apps, err)
	}
}

func TestListInstalledAppsReturnsEmptyForMissingPrefix(t *testing.T) {
	apps, err := listInstalledApps(filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(apps) != 0 {
		t.Fatalf("apps = %#v", apps)
	}
}

func TestRemoveManagedPortableAppDoesNotTouchSibling(t *testing.T) {
	home := t.TempDir()
	prefix := filepath.Join(home, "Programs Wine")
	appDir := filepath.Join(prefix, "portable-app")
	sibling := filepath.Join(prefix, "sibling", "keep.txt")
	if err := os.MkdirAll(filepath.Dir(sibling), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sibling, []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(appConfig{Name: "Portable App", Target: filepath.Join(appDir, "app.exe"), Prefix: prefix, Type: "portable"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "config.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "app.exe"), []byte("portable"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "user-data.txt"), []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(home, ".local", "share", "applications", "portable-app.desktop")
	if err := os.MkdirAll(filepath.Dir(launcher), 0755); err != nil {
		t.Fatal(err)
	}
	launcherData := "[Desktop Entry]\nExec=env " + desktopExecArg("WINEPREFIX="+filepath.Dir(filepath.Dir(filepath.Join(appDir, "app.exe")))) + " wine " + desktopExecArg(filepath.Join(appDir, "app.exe")) + "\nX-WindowsInstaller-Managed=true\n"
	if err := os.WriteFile(launcher, []byte(launcherData), 0644); err != nil {
		t.Fatal(err)
	}
	if err := removeManagedApp(home, prefix, "portable-app"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(appDir); !os.IsNotExist(err) {
		t.Fatalf("managed app directory still exists: %v", err)
	}
	if _, err := os.Stat(sibling); err != nil {
		t.Fatalf("sibling was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(appDir, "user-data.txt")); !os.IsNotExist(err) {
		t.Fatalf("nested app file still exists: %v", err)
	}
	if _, err := os.Stat(launcher); !os.IsNotExist(err) {
		t.Fatalf("owned menu launcher still exists: %v", err)
	}
}

func TestRemoveManagedPortableAppPreservesUnownedMenuLauncher(t *testing.T) {
	home := t.TempDir()
	prefix := filepath.Join(home, "Programs Wine")
	appDir := filepath.Join(prefix, "portable-app")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(appDir, "app.exe")
	if err := os.WriteFile(target, nil, 0755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(appConfig{Name: "Portable App", Target: target, Prefix: prefix, Type: "portable"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "config.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(home, ".local", "share", "applications", "portable-app.desktop")
	if err := os.MkdirAll(filepath.Dir(launcher), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(launcher, []byte("[Desktop Entry]\nName=Other App\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := removeManagedApp(home, prefix, "portable-app"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(launcher); err != nil {
		t.Fatalf("unowned launcher was removed: %v", err)
	}
}

func TestRemoveManagedAppRejectsConfigNameThatTargetsAnotherShortcut(t *testing.T) {
	home := t.TempDir()
	prefix := filepath.Join(home, "Programs Wine")
	appDir := filepath.Join(prefix, "portable-app")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(appConfig{Name: "Other App", Target: filepath.Join(appDir, "app.exe"), Prefix: prefix, Type: "portable"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "config.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	if err := removeManagedApp(home, prefix, "portable-app"); err == nil {
		t.Fatal("expected mismatched managed name to be rejected")
	}
}

func TestRemoveManagedAppRejectsSymlinkedManagedConfig(t *testing.T) {
	home := t.TempDir()
	prefix := filepath.Join(home, "Programs Wine")
	appDir := filepath.Join(prefix, "portable-app")
	outside := filepath.Join(home, "outside-config.json")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(appConfig{Name: "Portable App", Prefix: prefix})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, data, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(appDir, "config.json")); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	if err := removeManagedApp(home, prefix, "portable-app"); err == nil {
		t.Fatal("expected symlinked managed config to be rejected")
	}
}

func TestFrontendHasNoSecondaryExecutableSelector(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("frontend", "src", "App.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, forbidden := range []string{"SelectExecutable", "Elegir archivo", "Ejecutable instalado", "Definí cómo usarla", "Aplicación portable", "Nombre visible", "Icono"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("frontend contains forbidden text %q", forbidden)
		}
	}
}
