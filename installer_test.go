package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestCopyPortableExecutableUsesApplicationDirectoryAndSafeTarget(t *testing.T) {
	home := t.TempDir()
	prefix := filepath.Join(home, "programas")
	source := filepath.Join(home, "Downloads", "portable.exe")
	if err := os.MkdirAll(filepath.Dir(source), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("portable"), 0600); err != nil {
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
	if original, err := os.ReadFile(source); err != nil || string(original) != "portable" {
		t.Fatalf("source executable changed: %q, err = %v", original, err)
	}
	entry := desktopEntryContent("Mi App", target, prefix)
	if !strings.Contains(entry, "wine \""+target+"\"") {
		t.Fatalf("desktop target = %s", entry)
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

func TestWriteAppFilesPersistsIconAndDesktop(t *testing.T) {
	home := t.TempDir()
	prefix := filepath.Join(home, "programas")
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
	shortcut, err := writeAppFiles(home, req, target, prefix)
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(shortcut)
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	if !strings.Contains(text, "Icon=application-x-executable") {
		t.Fatalf("desktop entry = %s", text)
	}
	if _, err := os.Stat(filepath.Join(prefix, "mi-app", "config.json")); err != nil {
		t.Fatal(err)
	}
	desktop, err := desktopDirectory(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(desktop, "mi-app.desktop")); err != nil {
		t.Fatalf("Desktop shortcut = %v", err)
	}
	if !strings.Contains(text, "StartupNotify=true") {
		t.Fatalf("desktop entry missing startup notification: %s", text)
	}
	config, err := os.ReadFile(filepath.Join(prefix, "mi-app", "config.json"))
	if err != nil || !strings.Contains(string(config), `"target": "`+target+`"`) || !strings.Contains(string(config), `"prefix": "`+prefix+`"`) {
		t.Fatalf("app config = %s, err = %v", config, err)
	}
}

func TestWriteAppFilesUsesFallbackIconAndDesktop(t *testing.T) {
	home := t.TempDir()
	prefix := filepath.Join(home, "programas")
	req := InstallRequest{InstallerPath: filepath.Join(home, "Fallback App.exe")}
	shortcut, err := writeAppFiles(home, req, filepath.Join(prefix, "drive_c", "app.exe"), prefix)
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(shortcut)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "Icon=application-x-executable") {
		t.Fatalf("desktop entry = %s", content)
	}
	if _, err := os.Stat(filepath.Join(home, "Desktop", "fallback-app.desktop")); err != nil {
		t.Fatalf("Desktop shortcut = %v", err)
	}
}

type fakeProcess struct{}

func (fakeProcess) Wait() error { return nil }

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
	app := &App{startEnv: func(_ context.Context, env []string, name string, args ...string) (CommandProcess, error) {
		gotEnv, gotName, gotArgs = env, name, args
		return fakeProcess{}, nil
	}, emit: func(context.Context, string, ...interface{}) {}}
	result := app.Install(InstallRequest{InstallerPath: path})
	if !result.Success {
		t.Fatalf("result = %#v", result)
	}
	if gotName != "wine" || strings.Join(gotArgs, "\x00") != strings.Join([]string{"msiexec", "/i", path}, "\x00") || gotEnv[0] != "WINEPREFIX="+filepath.Join(home, "programas") {
		t.Fatalf("command=%s %#v env=%#v", gotName, gotArgs, gotEnv)
	}
}

func TestApplicationNameIsDerivedWithoutExtension(t *testing.T) {
	if got := applicationName(filepath.Join("/tmp", "Mi.App.msi")); got != "Mi.App" {
		t.Fatalf("application name = %q", got)
	}
}

func TestUninstallUsesWinePrefixAndOfficialCommand(t *testing.T) {
	home := t.TempDir()
	old := userHomeDir
	userHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { userHomeDir = old })
	var gotName string
	var gotArgs, gotEnv []string
	app := &App{startEnv: func(_ context.Context, env []string, name string, args ...string) (CommandProcess, error) {
		gotEnv, gotName, gotArgs = env, name, args
		return fakeProcess{}, nil
	}, emit: func(context.Context, string, ...interface{}) {}}
	result := app.Uninstall()
	if !result.Success {
		t.Fatalf("result = %#v", result)
	}
	if gotName != "wine" || strings.Join(gotArgs, "\x00") != "uninstaller" || strings.Join(gotEnv, "\x00") != "WINEPREFIX="+filepath.Join(home, "programas") {
		t.Fatalf("command=%s %#v env=%#v", gotName, gotArgs, gotEnv)
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
