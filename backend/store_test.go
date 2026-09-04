package backend

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSanitizeProfileNameSupportsUnicode(t *testing.T) {
	tests := map[string]string{
		"Польша":             "Польша",
		"Литва 2":            "Литва 2",
		"Сервер_Москва-01":   "Сервер_Москва-01",
		" Germany ":          "Germany",
		"Польша/../Россия":   "ПольшаРоссия",
		"Украина\\server":   "Украинаserver",
	}
	for input, want := range tests {
		if got := sanitizeProfileName(input); got != want {
			t.Fatalf("sanitizeProfileName(%q)=%q, want %q", input, got, want)
		}
	}
}

func TestStoreSaveAndLoadUnicodeProfileName(t *testing.T) {
	store := NewTestStore(t)
	name := "Польша"
	profile := ProfileData{
		PeerAddr: "127.0.0.1:56000",
		Password: "secret",
		Hashes:   []string{"AAAAAAAAAAAAAAAA"},
	}
	if err := store.SaveProfile(name, profile); err != nil {
		t.Fatalf("SaveProfile(%q): %v", name, err)
	}
	got, err := store.LoadProfile(name)
	if err != nil {
		t.Fatalf("LoadProfile(%q): %v", name, err)
	}
	if got.PeerAddr != profile.PeerAddr || got.Password != profile.Password {
		t.Fatalf("loaded profile mismatch: %+v", got)
	}
	path := filepath.Join(store.baseDir, "servers", name+".json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("unicode profile file not created: %v", err)
	}
}
