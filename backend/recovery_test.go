package backend

import (
    "bytes"
    "errors"
    "os"
    "path/filepath"
    "strings"
    "sync"
    "testing"
)

func quarantinedCopies(t *testing.T, path string) []string {
    t.Helper()
    matches, err := filepath.Glob(path + ".corrupt-*.bak")
    if err != nil { t.Fatal(err) }
    return matches
}

func TestIssue25MissingSettingsDoNotTriggerRecovery(t *testing.T) {
    store := NewTestStore(t)
    got := store.LoadSettings()
    if !got.AutoStart || got.ObfsMode != "audio" { t.Fatalf("unexpected defaults: %+v", got) }
    if len(store.RecoveryNotices()) != 0 { t.Fatal("missing settings were classified as corrupt") }
    if len(quarantinedCopies(t, filepath.Join(store.baseDir, "config.json"))) != 0 { t.Fatal("unexpected backup") }
}

func TestIssue25SettingsQuarantineBeforeSave(t *testing.T) {
    store := NewTestStore(t)
    path := filepath.Join(store.baseDir, "config.json")
    original := []byte("{ invalid JSON: private user data }")
    if err := os.WriteFile(path, original, 0o600); err != nil { t.Fatal(err) }
    got := store.LoadSettings()
    if !got.AutoStart || got.ObfsMode != "audio" { t.Fatalf("recovery did not use defaults: %+v", got) }
    copies := quarantinedCopies(t, path)
    if len(copies) != 1 { t.Fatalf("backup count = %d, want 1", len(copies)) }
    backup, err := os.ReadFile(copies[0])
    if err != nil || !bytes.Equal(backup, original) { t.Fatalf("recovery changed original bytes: %q, %v", backup, err) }
    if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) { t.Fatalf("corrupt file still active: %v", err) }
    notices := store.RecoveryNotices()
    if len(notices) != 1 || !strings.Contains(notices[0], "резервной копии") { t.Fatalf("notice not retained: %v", notices) }
    if strings.Contains(strings.Join(notices, ""), store.baseDir) || strings.Contains(strings.Join(notices, ""), "private user data") {
        t.Fatal("notice includes sensitive data")
    }
    want := AppSettings{AutoStart: false, ObfsAccepted: true, ObfsMode: "video", DebugLogging: true}
    if err := store.SaveSettings(want); err != nil { t.Fatal(err) }
    if loaded := store.LoadSettings(); loaded != want { t.Fatalf("settings not restored: %+v", loaded) }
    backupAfter, err := os.ReadFile(copies[0])
    if err != nil || !bytes.Equal(backupAfter, original) { t.Fatal("backup was modified by SaveSettings") }
}

func TestIssue25MalformedSchemaQuarantine(t *testing.T) {
    store := NewTestStore(t)
    path := filepath.Join(store.baseDir, "config.json")
    if err := os.WriteFile(path, []byte(`{"autoStart":"not a bool"}`), 0o600); err != nil { t.Fatal(err) }
    _ = store.LoadSettings()
    if len(quarantinedCopies(t, path)) != 1 { t.Fatal("schema error was not quarantined") }
}

func TestIssue25ProfileQuarantinePreservesValidProfiles(t *testing.T) {
    store := NewTestStore(t)
    valid := ProfileData{PeerAddr: "127.0.0.1:4567", Password: "test"}
    if err := store.SaveProfile("working", valid); err != nil { t.Fatal(err) }
    path := filepath.Join(store.baseDir, "servers", "broken.json")
    original := []byte(`{"peer":"127.0.0.1","password":`)
    if err := os.WriteFile(path, original, 0o600); err != nil { t.Fatal(err) }
    profiles := store.ListProfiles()
    if len(profiles) != 1 || profiles["working"].PeerAddr != valid.PeerAddr { t.Fatalf("valid profiles lost: %+v", profiles) }
    copies := quarantinedCopies(t, path)
    if len(copies) != 1 { t.Fatalf("missing quarantine copy: %v", copies) }
    data, err := os.ReadFile(copies[0])
    if err != nil || !bytes.Equal(data, original) { t.Fatal("profile backup is incomplete") }
    if len(store.RecoveryNotices()) != 1 { t.Fatalf("missing profile recovery notice: %v", store.RecoveryNotices()) }
    if _, err := store.LoadProfile("broken"); !errors.Is(err, os.ErrNotExist) { t.Fatalf("expected missing profile after quarantine: %v", err) }
    if err := store.SaveProfile("broken", ProfileData{PeerAddr:"localhost:33"}); err != nil { t.Fatal(err) }
    loaded, err := store.LoadProfile("broken")
    if err != nil || loaded.PeerAddr != "localhost:33" { t.Fatalf("new profile unavailable: %+v %v", loaded, err) }
    if loaded.DeviceID == "" { t.Fatal("new profile missing device ID") }
    data, err = os.ReadFile(copies[0])
    if err != nil || !bytes.Equal(data, original) { t.Fatal("backup changed after profile replacement") }
}

func TestIssue25CannotOverwriteUnreadableSettings(t *testing.T) {
    store := NewTestStore(t)
    path := filepath.Join(store.baseDir, "config.json")
    if err := os.Mkdir(path, 0o700); err != nil { t.Fatal(err) }
    _ = store.LoadSettings()
    if err := store.SaveSettings(AppSettings{AutoStart: true}); err == nil { t.Fatal("unreadable active config was overwritten") }
    info, err := os.Stat(path)
    if err != nil || !info.IsDir() { t.Fatal("unreadable config was damaged") }
    if len(store.RecoveryNotices()) == 0 { t.Fatal("read failure was silent") }
}

func TestIssue25ConcurrentRecoveryCreatesSingleBackup(t *testing.T) {
    store := NewTestStore(t)
    path := filepath.Join(store.baseDir, "config.json")
    if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil { t.Fatal(err) }
    var wg sync.WaitGroup
    for i:=0; i<16; i++ {
        wg.Add(1)
        go func(){ defer wg.Done(); _ = store.LoadSettings() }()
    }
    wg.Wait()
    if copies:=quarantinedCopies(t, path); len(copies)!=1 { t.Fatalf("concurrent quarantine backups: %v", copies) }
    if len(store.RecoveryNotices()) != 1 { t.Fatalf("duplicate notices: %v", store.RecoveryNotices()) }
}
