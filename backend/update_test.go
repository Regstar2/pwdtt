package backend

import (
	"net/url"
	"testing"
)

func TestIsNewer(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		want bool
	}{
		{name: "equal", a: "1.7.0", b: "1.7.0", want: false},
		{name: "patch", a: "1.7.1", b: "1.7.0", want: true},
		{name: "minor", a: "1.8.0", b: "1.7.9", want: true},
		{name: "major", a: "2.0.0", b: "1.99.99", want: true},
		{name: "v prefix", a: "v1.8.0", b: "1.7.0", want: true},
		{name: "older", a: "1.6.9", b: "1.7.0", want: false},
		{name: "stable newer than prerelease", a: "1.8.0", b: "1.8.0-rc.1", want: true},
		{name: "prerelease order", a: "1.8.0-rc.2", b: "1.8.0-rc.1", want: true},
		{name: "build metadata ignored", a: "1.8.0+build.2", b: "1.8.0+build.1", want: false},
		{name: "dev is not semver", a: "dev", b: "1.7.0", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isNewer(test.a, test.b); got != test.want {
				t.Fatalf("isNewer(%q, %q) = %v, want %v", test.a, test.b, got, test.want)
			}
		})
	}
}

func TestComparableVersionRejectsInvalidSemver(t *testing.T) {
	for _, version := range []string{"dev", "1.2", "1.02.3", "1.2.3-01", "1.2.3+"} {
		if isComparableVersion(version) {
			t.Fatalf("isComparableVersion(%q) = true, want false", version)
		}
	}
}

func TestSelectReleaseAsset(t *testing.T) {
	assets := []releaseAsset{
		{Name: "pwdtt-linux-amd64", BrowserDownloadURL: "https://github.com/Regstar2/PWDTT/releases/download/v1.8.0/pwdtt-linux-amd64"},
		{Name: "pwdtt-windows-amd64.exe", BrowserDownloadURL: "https://github.com/Regstar2/PWDTT/releases/download/v1.8.0/pwdtt-windows-amd64.exe"},
		{Name: "PWDTT-macos.zip", BrowserDownloadURL: "https://github.com/Regstar2/PWDTT/releases/download/v1.8.0/PWDTT-macos.zip"},
	}

	tests := []struct {
		name   string
		goos   string
		goarch string
		want   string
	}{
		{name: "windows amd64", goos: "windows", goarch: "amd64", want: assets[1].BrowserDownloadURL},
		{name: "linux amd64", goos: "linux", goarch: "amd64", want: assets[0].BrowserDownloadURL},
		{name: "mac amd64 universal", goos: "darwin", goarch: "amd64", want: assets[2].BrowserDownloadURL},
		{name: "mac arm64 universal", goos: "darwin", goarch: "arm64", want: assets[2].BrowserDownloadURL},
		{name: "windows arm64 must not use amd64", goos: "windows", goarch: "arm64", want: ""},
		{name: "linux arm64 must not use amd64", goos: "linux", goarch: "arm64", want: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := selectReleaseAsset(assets, test.goos, test.goarch); got != test.want {
				t.Fatalf("selectReleaseAsset(%s/%s) = %q, want %q", test.goos, test.goarch, got, test.want)
			}
		})
	}
}

func TestSelectReleaseAssetRejectsUntrustedURL(t *testing.T) {
	assets := []releaseAsset{{
		Name:               "pwdtt-windows-amd64.exe",
		BrowserDownloadURL: "https://example.com/pwdtt-windows-amd64.exe",
	}}
	if got := selectReleaseAsset(assets, "windows", "amd64"); got != "" {
		t.Fatalf("selectReleaseAsset returned untrusted URL %q", got)
	}
}

func TestEvaluateReleaseCurrentEqualsLatest(t *testing.T) {
	info, err := evaluateRelease(githubRelease{TagName: "v1.7.0"}, "1.7.0", "windows", "amd64")
	if err != nil {
		t.Fatalf("evaluateRelease: %v", err)
	}
	if info.Available {
		t.Fatalf("current == latest must not be available")
	}
	if info.Version != "1.7.0" {
		t.Fatalf("version = %q, want 1.7.0", info.Version)
	}
}

func TestEvaluateReleaseCurrentOlderThanLatest(t *testing.T) {
	assetURL := "https://github.com/Regstar2/PWDTT/releases/download/v1.8.0/pwdtt-windows-amd64.exe"
	info, err := evaluateRelease(githubRelease{
		TagName: "v1.8.0",
		Body:    "changes",
		Assets: []releaseAsset{{
			Name:               "pwdtt-windows-amd64.exe",
			BrowserDownloadURL: assetURL,
		}},
	}, "1.7.0", "windows", "amd64")
	if err != nil {
		t.Fatalf("evaluateRelease: %v", err)
	}
	if !info.Available {
		t.Fatalf("newer release with compatible asset must be available")
	}
	if info.Version != "1.8.0" || info.URL != assetURL || info.Body != "changes" {
		t.Fatalf("unexpected update info: %+v", info)
	}
}

func TestEvaluateReleaseWithoutCompatibleAsset(t *testing.T) {
	info, err := evaluateRelease(githubRelease{
		TagName: "v1.8.0",
		Assets: []releaseAsset{{
			Name:               "pwdtt-linux-amd64",
			BrowserDownloadURL: "https://github.com/Regstar2/PWDTT/releases/download/v1.8.0/pwdtt-linux-amd64",
		}},
	}, "1.7.0", "windows", "amd64")
	if err != nil {
		t.Fatalf("evaluateRelease: %v", err)
	}
	if info.Available {
		t.Fatalf("release without compatible asset must not be available")
	}
	if info.URL != "" {
		t.Fatalf("URL = %q, want empty", info.URL)
	}
}

func TestDevVersionFallbackIsExplicit(t *testing.T) {
	if Version != "dev" {
		t.Fatalf("source fallback Version = %q, want dev", Version)
	}
	if isComparableVersion(Version) {
		t.Fatalf("dev build must not be treated as release semver")
	}
}


func TestReleaseTagFromURL(t *testing.T) {
	tests := []struct {
		name    string
		rawURL  string
		want    string
		wantErr bool
	}{
		{name: "official latest redirect", rawURL: "https://github.com/Regstar2/PWDTT/releases/tag/v1.7.0", want: "v1.7.0"},
		{name: "case insensitive owner repo", rawURL: "https://github.com/regstar2/pwdtt/releases/tag/v1.8.0", want: "v1.8.0"},
		{name: "wrong repository", rawURL: "https://github.com/luminescq/PWDTT/releases/tag/v1.7.0", wantErr: true},
		{name: "wrong host", rawURL: "https://example.com/Regstar2/PWDTT/releases/tag/v1.7.0", wantErr: true},
		{name: "nested tag path", rawURL: "https://github.com/Regstar2/PWDTT/releases/tag/v1.7.0/extra", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parsed, err := url.Parse(test.rawURL)
			if err != nil {
				t.Fatalf("url.Parse: %v", err)
			}
			got, err := releaseTagFromURL(parsed)
			if test.wantErr {
				if err == nil {
					t.Fatalf("releaseTagFromURL(%q) = %q, want error", test.rawURL, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("releaseTagFromURL(%q): %v", test.rawURL, err)
			}
			if got != test.want {
				t.Fatalf("releaseTagFromURL(%q) = %q, want %q", test.rawURL, got, test.want)
			}
		})
	}
}

func TestKnownReleaseAssetName(t *testing.T) {
	tests := []struct {
		goos   string
		goarch string
		want   string
	}{
		{goos: "windows", goarch: "amd64", want: "pwdtt-windows-amd64.exe"},
		{goos: "linux", goarch: "amd64", want: "pwdtt-linux-amd64"},
		{goos: "darwin", goarch: "amd64", want: "PWDTT-macos.zip"},
		{goos: "darwin", goarch: "arm64", want: "PWDTT-macos.zip"},
		{goos: "windows", goarch: "arm64", want: ""},
		{goos: "linux", goarch: "arm64", want: ""},
	}

	for _, test := range tests {
		if got := knownReleaseAssetName(test.goos, test.goarch); got != test.want {
			t.Fatalf("knownReleaseAssetName(%s/%s) = %q, want %q", test.goos, test.goarch, got, test.want)
		}
	}
}


func TestReleaseAssetURL(t *testing.T) {
	got, err := releaseAssetURL("v1.7.0", "pwdtt-windows-amd64.exe")
	if err != nil {
		t.Fatalf("releaseAssetURL: %v", err)
	}
	want := "https://github.com/Regstar2/PWDTT/releases/download/v1.7.0/pwdtt-windows-amd64.exe"
	if got != want {
		t.Fatalf("releaseAssetURL = %q, want %q", got, want)
	}

	for _, assetName := range []string{"", "../pwdtt.exe", "dir/pwdtt.exe", "dir\\pwdtt.exe"} {
		if _, err := releaseAssetURL("v1.7.0", assetName); err == nil {
			t.Fatalf("releaseAssetURL accepted invalid asset name %q", assetName)
		}
	}
}
