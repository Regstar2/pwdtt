package backend

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	updateAPIURL     = "https://api.github.com/repos/Regstar2/PWDTT/releases/latest"
	updateReleaseURL = "https://github.com/Regstar2/PWDTT/releases/latest"
	maxReleaseBody   = 2 << 20
)

// UpdateInfo describes an installable update for the current platform.
type UpdateInfo struct {
	Available bool   `json:"available"`
	Version   string `json:"version"`
	URL       string `json:"url"`
	Body      string `json:"body"`
}

type releaseAsset struct {
	BrowserDownloadURL string `json:"browser_download_url"`
	Name               string `json:"name"`
}

type githubRelease struct {
	TagName string         `json:"tag_name"`
	Body    string         `json:"body"`
	Assets  []releaseAsset `json:"assets"`
}

// CheckUpdate checks the stable release channel of the maintained fork.
func CheckUpdate(currentVersion string) (*UpdateInfo, error) {
	if !isComparableVersion(currentVersion) {
		return nil, fmt.Errorf("current version %q is not a release semver", currentVersion)
	}

	request, err := http.NewRequest(http.MethodGet, updateAPIURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create update request: %w", err)
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "Regstar2-PWDTT-update-checker")

	client := &http.Client{Timeout: 8 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("update request: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github update API status: %d", response.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, maxReleaseBody+1))
	if err != nil {
		return nil, fmt.Errorf("read update response: %w", err)
	}
	if len(body) > maxReleaseBody {
		return nil, fmt.Errorf("update response is too large")
	}

	var release githubRelease
	if err := json.Unmarshal(body, &release); err != nil {
		return nil, fmt.Errorf("parse update response: %w", err)
	}

	info, err := evaluateRelease(release, currentVersion, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return nil, err
	}
	log.Printf("[UPDATE] source=Regstar2/PWDTT current=%s latest=%s platform=%s/%s available=%v", currentVersion, info.Version, runtime.GOOS, runtime.GOARCH, info.Available)
	return info, nil
}

func evaluateRelease(release githubRelease, currentVersion, goos, goarch string) (*UpdateInfo, error) {
	latest := strings.TrimPrefix(strings.TrimSpace(release.TagName), "v")
	if !isComparableVersion(latest) {
		return nil, fmt.Errorf("latest release tag %q is not a supported semver", release.TagName)
	}
	if !isNewer(latest, currentVersion) {
		return &UpdateInfo{Available: false, Version: latest}, nil
	}

	downloadURL := selectReleaseAsset(release.Assets, goos, goarch)
	if downloadURL == "" {
		log.Printf("[UPDATE] newer release %s has no compatible asset for %s/%s; see %s", latest, goos, goarch, updateReleaseURL)
		return &UpdateInfo{Available: false, Version: latest}, nil
	}

	return &UpdateInfo{
		Available: true,
		Version:   latest,
		URL:       downloadURL,
		Body:      release.Body,
	}, nil
}

func selectReleaseAsset(assets []releaseAsset, goos, goarch string) string {
	for _, asset := range assets {
		if !isOfficialReleaseAssetURL(asset.BrowserDownloadURL) {
			continue
		}

		name := strings.ToLower(strings.TrimSpace(asset.Name))
		switch goos {
		case "windows":
			if goarch == "amd64" && strings.Contains(name, "windows") && containsAny(name, "amd64", "x86_64", "x64") {
				return asset.BrowserDownloadURL
			}
		case "linux":
			if goarch == "amd64" && strings.Contains(name, "linux") && containsAny(name, "amd64", "x86_64", "x64") {
				return asset.BrowserDownloadURL
			}
		case "darwin":
			if goarch != "amd64" && goarch != "arm64" {
				continue
			}
			if name == "pwdtt-macos.zip" {
				return asset.BrowserDownloadURL
			}
			if !containsAny(name, "darwin", "macos", "mac") {
				continue
			}
			if strings.Contains(name, "universal") || containsAny(name, archAliases(goarch)...) {
				return asset.BrowserDownloadURL
			}
		}
	}
	return ""
}

func isOfficialReleaseAssetURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || !strings.EqualFold(parsed.Host, "github.com") {
		return false
	}
	return strings.HasPrefix(strings.ToLower(parsed.EscapedPath()), "/regstar2/pwdtt/releases/download/")
}

func archAliases(arch string) []string {
	switch arch {
	case "amd64":
		return []string{"amd64", "x86_64", "x64"}
	case "arm64":
		return []string{"arm64", "aarch64"}
	default:
		return []string{arch}
	}
}

func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if candidate != "" && strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}

type semanticVersion struct {
	major int
	minor int
	patch int
	pre   []semverIdentifier
}

type semverIdentifier struct {
	value   string
	numeric bool
	number  int
}

func isComparableVersion(version string) bool {
	_, ok := parseSemanticVersion(version)
	return ok
}

// isNewer reports whether a is a newer semantic version than b.
func isNewer(a, b string) bool {
	left, leftOK := parseSemanticVersion(a)
	right, rightOK := parseSemanticVersion(b)
	if !leftOK || !rightOK {
		return false
	}
	return compareSemanticVersions(left, right) > 0
}

func parseSemanticVersion(version string) (semanticVersion, bool) {
	value := strings.TrimSpace(version)
	value = strings.TrimPrefix(value, "v")
	if value == "" {
		return semanticVersion{}, false
	}

	if plus := strings.IndexByte(value, '+'); plus >= 0 {
		metadata := value[plus+1:]
		if !validIdentifierList(metadata, false) {
			return semanticVersion{}, false
		}
		value = value[:plus]
	}

	var preRaw string
	if dash := strings.IndexByte(value, '-'); dash >= 0 {
		preRaw = value[dash+1:]
		value = value[:dash]
		if !validIdentifierList(preRaw, true) {
			return semanticVersion{}, false
		}
	}

	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return semanticVersion{}, false
	}
	major, ok := parseCoreNumber(parts[0])
	if !ok {
		return semanticVersion{}, false
	}
	minor, ok := parseCoreNumber(parts[1])
	if !ok {
		return semanticVersion{}, false
	}
	patch, ok := parseCoreNumber(parts[2])
	if !ok {
		return semanticVersion{}, false
	}

	result := semanticVersion{major: major, minor: minor, patch: patch}
	if preRaw == "" {
		return result, true
	}
	for _, raw := range strings.Split(preRaw, ".") {
		identifier := semverIdentifier{value: raw}
		if isDigits(raw) {
			number, err := strconv.Atoi(raw)
			if err != nil {
				return semanticVersion{}, false
			}
			identifier.numeric = true
			identifier.number = number
		}
		result.pre = append(result.pre, identifier)
	}
	return result, true
}

func parseCoreNumber(value string) (int, bool) {
	if value == "" || !isDigits(value) || (len(value) > 1 && value[0] == '0') {
		return 0, false
	}
	number, err := strconv.Atoi(value)
	return number, err == nil
}

func validIdentifierList(value string, prerelease bool) bool {
	if value == "" {
		return false
	}
	for _, part := range strings.Split(value, ".") {
		if part == "" {
			return false
		}
		for _, char := range part {
			if !((char >= '0' && char <= '9') || (char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z') || char == '-') {
				return false
			}
		}
		if prerelease && isDigits(part) && len(part) > 1 && part[0] == '0' {
			return false
		}
	}
	return true
}

func isDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func compareSemanticVersions(a, b semanticVersion) int {
	if result := compareInt(a.major, b.major); result != 0 {
		return result
	}
	if result := compareInt(a.minor, b.minor); result != 0 {
		return result
	}
	if result := compareInt(a.patch, b.patch); result != 0 {
		return result
	}
	if len(a.pre) == 0 && len(b.pre) == 0 {
		return 0
	}
	if len(a.pre) == 0 {
		return 1
	}
	if len(b.pre) == 0 {
		return -1
	}

	limit := len(a.pre)
	if len(b.pre) < limit {
		limit = len(b.pre)
	}
	for index := 0; index < limit; index++ {
		left := a.pre[index]
		right := b.pre[index]
		if left.numeric && right.numeric {
			if result := compareInt(left.number, right.number); result != 0 {
				return result
			}
			continue
		}
		if left.numeric != right.numeric {
			if left.numeric {
				return -1
			}
			return 1
		}
		if left.value < right.value {
			return -1
		}
		if left.value > right.value {
			return 1
		}
	}
	return compareInt(len(a.pre), len(b.pre))
}

func compareInt(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
