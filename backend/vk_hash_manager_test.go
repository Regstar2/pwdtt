package backend

import (
	"testing"
	"time"
)

func TestDefaultVKHashPolicyPreservesLegacyProfiles(t *testing.T) {
	policy := normalizeVKHashPolicy(nil)
	if policy.Mode != vkHashModeLocal || !policy.AutoCheck || policy.AutoReplace {
		t.Fatalf("unexpected default policy: %+v", policy)
	}
}

func TestSelectVKHashCandidatesModesAndDeduplicates(t *testing.T) {
	local := []VKHashEntry{
		{ID: "l1", Hash: "AAAAAAAAAAAAAAAA"},
		{ID: "l2", Hash: "BBBBBBBBBBBBBBBB"},
	}
	pool := []VKHashEntry{
		{ID: "p1", Hash: "BBBBBBBBBBBBBBBB", InPool: true},
		{ID: "p2", Hash: "CCCCCCCCCCCCCCCC", InPool: true},
	}

	localOnly := selectVKHashCandidates(local, pool, vkHashModeLocal)
	if len(localOnly) != 2 || localOnly[0].Hash != local[0].Hash || localOnly[1].Hash != local[1].Hash {
		t.Fatalf("local candidates: %+v", localOnly)
	}

	poolOnly := selectVKHashCandidates(local, pool, vkHashModePool)
	if len(poolOnly) != 2 || poolOnly[0].Hash != pool[0].Hash || poolOnly[1].Hash != pool[1].Hash {
		t.Fatalf("pool candidates: %+v", poolOnly)
	}

	mixed := selectVKHashCandidates(local, pool, vkHashModeLocalAndPool)
	if len(mixed) != 3 {
		t.Fatalf("mixed candidate count=%d, want 3: %+v", len(mixed), mixed)
	}
	if mixed[0].Hash != "AAAAAAAAAAAAAAAA" || mixed[1].Hash != "BBBBBBBBBBBBBBBB" || mixed[2].Hash != "CCCCCCCCCCCCCCCC" {
		t.Fatalf("mixed order/dedup mismatch: %+v", mixed)
	}
}

func TestVKHashCheckTTL(t *testing.T) {
	now := time.Now()
	valid := VKHashCheck{Status: vkHashStatusValid, CheckedAt: now.Add(-time.Hour).UnixMilli()}
	if !isVKHashCheckFresh(valid, now) {
		t.Fatal("one-hour valid check should be fresh")
	}
	stale := VKHashCheck{Status: vkHashStatusValid, CheckedAt: now.Add(-5 * time.Hour).UnixMilli()}
	if isVKHashCheckFresh(stale, now) {
		t.Fatal("five-hour valid check should be stale")
	}
	errorRecent := VKHashCheck{Status: vkHashStatusError, CheckedAt: now.Add(-time.Minute).UnixMilli()}
	if !isVKHashCheckFresh(errorRecent, now) {
		t.Fatal("recent infrastructure error should use short TTL")
	}
	errorStale := VKHashCheck{Status: vkHashStatusError, CheckedAt: now.Add(-10 * time.Minute).UnixMilli()}
	if isVKHashCheckFresh(errorStale, now) {
		t.Fatal("old infrastructure error should be retried")
	}
}

func TestProfilesUsingPoolAndLocalHash(t *testing.T) {
	entry := VKHashEntry{Hash: "AAAAAAAAAAAAAAAA", InPool: true}
	profiles := map[string]ProfileData{
		"local": {
			Hashes:     []string{"AAAAAAAAAAAAAAAA"},
			HashPolicy: &VKHashPolicy{Mode: vkHashModeLocal, AutoCheck: true},
		},
		"pool": {
			HashPolicy: &VKHashPolicy{Mode: vkHashModePool, AutoCheck: true},
		},
		"unrelated": {
			Hashes:     []string{"BBBBBBBBBBBBBBBB"},
			HashPolicy: &VKHashPolicy{Mode: vkHashModeLocal, AutoCheck: true},
		},
	}
	usedBy := profilesUsingVKHash(entry, profiles)
	if len(usedBy) != 2 || usedBy[0] != "local" || usedBy[1] != "pool" {
		t.Fatalf("usedBy=%v", usedBy)
	}
}
