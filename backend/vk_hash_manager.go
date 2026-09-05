package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	core "wg-turn-client"
)

const (
	vkHashModeLocal        = "local"
	vkHashModeLocalAndPool = "local+pool"
	vkHashModePool         = "pool"

	vkHashStatusValid   = "valid"
	vkHashStatusInvalid = "invalid"
	vkHashStatusError   = "error"

	vkHashCheckTTL      = 4 * time.Hour
	vkHashErrorTTL      = 5 * time.Minute
	vkHashProbeTimeout     = 30 * time.Second
	vkHashPreflightTimeout = 15 * time.Second
	maxAutoReplacements = 2
	vkHashRegistryName  = "vk-hashes.json"
)

type vkHashRegistry struct {
	Entries []VKHashEntry
}

var vkHashRegistryMu sync.Mutex

func defaultVKHashPolicy() VKHashPolicy {
	return VKHashPolicy{Mode: vkHashModeLocal, AutoCheck: true}
}

func normalizeVKHashMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case vkHashModePool:
		return vkHashModePool
	case vkHashModeLocalAndPool:
		return vkHashModeLocalAndPool
	default:
		return vkHashModeLocal
	}
}

func normalizeVKHashPolicy(policy *VKHashPolicy) VKHashPolicy {
	if policy == nil {
		return defaultVKHashPolicy()
	}
	return VKHashPolicy{
		Mode:        normalizeVKHashMode(policy.Mode),
		AutoCheck:   policy.AutoCheck,
		AutoReplace: policy.AutoReplace,
	}
}

func hashPolicyFromConnect(params ConnectParams) VKHashPolicy {
	if strings.TrimSpace(params.HashMode) == "" {
		return defaultVKHashPolicy()
	}
	return VKHashPolicy{
		Mode:        normalizeVKHashMode(params.HashMode),
		AutoCheck:   params.HashAutoCheck,
		AutoReplace: params.HashAutoReplace,
	}
}

func normalizeVKHashList(values []string) ([]string, error) {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, raw := range values {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		hash, err := normalizeVKCallHash(raw)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[hash]; ok {
			continue
		}
		seen[hash] = struct{}{}
		result = append(result, hash)
	}
	return result, nil
}

func (s *Store) loadVKHashRegistry() (vkHashRegistry, error) {
	data, err := os.ReadFile(filepath.Join(s.baseDir, vkHashRegistryName))
	if errors.Is(err, os.ErrNotExist) {
		return vkHashRegistry{}, nil
	}
	if err != nil {
		return vkHashRegistry{}, err
	}
	var registry vkHashRegistry
	if err := json.Unmarshal(data, &registry); err != nil {
		return vkHashRegistry{}, fmt.Errorf("parse VK hash registry: %w", err)
	}
	for i := range registry.Entries {
		if registry.Entries[i].Checks == nil {
			registry.Entries[i].Checks = make(map[string]VKHashCheck)
		}
	}
	return registry, nil
}

func (s *Store) saveVKHashRegistry(registry vkHashRegistry) error {
	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(s.baseDir, vkHashRegistryName), data)
}

func findVKHashByID(registry *vkHashRegistry, id string) int {
	for i := range registry.Entries {
		if registry.Entries[i].ID == id {
			return i
		}
	}
	return -1
}

func findVKHashByValue(registry *vkHashRegistry, hash string) int {
	for i := range registry.Entries {
		if registry.Entries[i].Hash == hash {
			return i
		}
	}
	return -1
}

func normalizeVKHashSource(source string) string {
	switch strings.TrimSpace(source) {
	case "generated":
		return "generated"
	case "imported":
		return "imported"
	default:
		return "manual"
	}
}

func ensureVKHashEntry(registry *vkHashRegistry, raw, source string, inPool bool) (VKHashEntry, bool, error) {
	hash, err := normalizeVKCallHash(raw)
	if err != nil {
		return VKHashEntry{}, false, err
	}
	if index := findVKHashByValue(registry, hash); index >= 0 {
		entry := &registry.Entries[index]
		if inPool {
			entry.InPool = true
		}
		if entry.Source == "" {
			entry.Source = normalizeVKHashSource(source)
		}
		if entry.Checks == nil {
			entry.Checks = make(map[string]VKHashCheck)
		}
		return *entry, false, nil
	}
	entry := VKHashEntry{
		ID:        uuid.NewString(),
		Hash:      hash,
		Source:    normalizeVKHashSource(source),
		InPool:    inPool,
		CreatedAt: time.Now().UnixMilli(),
		Checks:    make(map[string]VKHashCheck),
	}
	registry.Entries = append(registry.Entries, entry)
	return entry, true, nil
}

func profileContainsVKHash(profile ProfileData, hash string) bool {
	for _, raw := range profile.Hashes {
		normalized, err := normalizeVKCallHash(raw)
		if err == nil && normalized == hash {
			return true
		}
	}
	return false
}

func profilesUsingVKHash(entry VKHashEntry, profiles map[string]ProfileData) []string {
	usedBy := make([]string, 0)
	for name, profile := range profiles {
		policy := normalizeVKHashPolicy(profile.HashPolicy)
		usesLocal := profileContainsVKHash(profile, entry.Hash)
		usesPool := entry.InPool && (policy.Mode == vkHashModePool || policy.Mode == vkHashModeLocalAndPool)
		if usesLocal || usesPool {
			usedBy = append(usedBy, name)
		}
	}
	sort.Strings(usedBy)
	return usedBy
}

func (a *App) registerLocalVKHashes(hashes []string) error {
	vkHashRegistryMu.Lock()
	defer vkHashRegistryMu.Unlock()
	registry, err := a.store.loadVKHashRegistry()
	if err != nil {
		return err
	}
	changed := false
	for _, raw := range hashes {
		_, created, err := ensureVKHashEntry(&registry, raw, "manual", false)
		if err != nil {
			return err
		}
		changed = changed || created
	}
	if changed {
		return a.store.saveVKHashRegistry(registry)
	}
	return nil
}

func (a *App) SyncProfileVKSettings(name, peerAddr, password string, hashes []string, mode string, autoCheck, autoReplace bool) error {
	name = sanitizeProfileName(name)
	if name == "" {
		return errors.New("invalid profile name")
	}
	normalized, err := normalizeVKHashList(hashes)
	if err != nil {
		return err
	}
	profile, err := a.store.LoadProfile(name)
	if err != nil {
		profile = &ProfileData{}
	}
	profile.PeerAddr = strings.TrimSpace(peerAddr)
	profile.Password = password
	profile.Hashes = normalized
	policy := VKHashPolicy{
		Mode:        normalizeVKHashMode(mode),
		AutoCheck:   autoCheck,
		AutoReplace: autoReplace,
	}
	if strings.TrimSpace(mode) == "" {
		policy = defaultVKHashPolicy()
	}
	profile.HashPolicy = &policy
	if err := a.store.SaveProfile(name, *profile); err != nil {
		return err
	}
	return a.registerLocalVKHashes(normalized)
}

func (a *App) ListVKHashes() []VKHashEntry {
	vkHashRegistryMu.Lock()
	registry, err := a.store.loadVKHashRegistry()
	vkHashRegistryMu.Unlock()
	if err != nil {
		return nil
	}
	profiles := a.store.ListProfiles()
	result := make([]VKHashEntry, 0, len(registry.Entries))
	for _, entry := range registry.Entries {
		entry.UsedBy = profilesUsingVKHash(entry, profiles)
		if entry.InPool || len(entry.UsedBy) > 0 {
			result = append(result, entry)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].InPool != result[j].InPool {
			return result[i].InPool
		}
		return result[i].CreatedAt > result[j].CreatedAt
	})
	return result
}

func (a *App) AddVKHash(raw, source, profileName string) (VKHashEntry, error) {
	vkHashRegistryMu.Lock()
	registry, err := a.store.loadVKHashRegistry()
	if err != nil {
		vkHashRegistryMu.Unlock()
		return VKHashEntry{}, err
	}
	entry, _, err := ensureVKHashEntry(&registry, raw, source, true)
	if err == nil {
		err = a.store.saveVKHashRegistry(registry)
	}
	vkHashRegistryMu.Unlock()
	if err != nil {
		return VKHashEntry{}, err
	}
	a.onBridgeEvent("vk-hash-pool-changed", map[string]any{"hashId": entry.ID, "action": "added"})
	if strings.TrimSpace(profileName) != "" {
		if a.hashOps == nil {
			a.hashOps = newVKHashOperationState()
		}
		go func(hashID, targetProfile string) {
			ctx, operationID, done := a.hashOps.beginManual(a.appContext())
			defer done()
			probeCtx, cancel := context.WithTimeout(ctx, vkHashProbeTimeout)
			defer cancel()
			_, _ = a.checkVKHashCoordinated(probeCtx, hashID, targetProfile, operationID)
		}(entry.ID, profileName)
	}
	return a.vkHashEntry(entry.ID)
}

func (a *App) vkHashEntry(id string) (VKHashEntry, error) {
	for _, entry := range a.ListVKHashes() {
		if entry.ID == id {
			return entry, nil
		}
	}
	return VKHashEntry{}, errors.New("VK-хеш не найден")
}

func (a *App) DeleteVKHash(id string) error {
	vkHashRegistryMu.Lock()
	defer vkHashRegistryMu.Unlock()
	registry, err := a.store.loadVKHashRegistry()
	if err != nil {
		return err
	}
	index := findVKHashByID(&registry, id)
	if index < 0 {
		return errors.New("VK-хеш не найден")
	}
	registry.Entries[index].InPool = false
	if len(profilesUsingVKHash(registry.Entries[index], a.store.ListProfiles())) == 0 {
		registry.Entries = append(registry.Entries[:index], registry.Entries[index+1:]...)
	}
	if err := a.store.saveVKHashRegistry(registry); err != nil {
		return err
	}
	a.onBridgeEvent("vk-hash-pool-changed", map[string]any{"hashId": id, "action": "removed"})
	return nil
}

func (a *App) CheckVKHash(id, profileName string) (VKHashCheckResult, error) {
	if a.hashOps == nil {
		a.hashOps = newVKHashOperationState()
	}
	ctx, operationID, done := a.hashOps.beginManual(a.appContext())
	defer done()
	return a.checkVKHashCoordinated(ctx, id, profileName, operationID)
}

func (a *App) CancelVKHashChecks() {
	if a.hashOps != nil {
		a.hashOps.cancelInteractive()
	}
}

func (a *App) CheckAllVKHashes(profileName string) ([]VKHashCheckResult, error) {
	profileName = sanitizeProfileName(profileName)
	if profileName == "" {
		return nil, errors.New("выберите сервер для проверки VK-хешей")
	}
	if a.hashOps == nil {
		a.hashOps = newVKHashOperationState()
	}

	ctx, operationID, done, err := a.hashOps.beginBulk(a.appContext())
	if err != nil {
		return nil, err
	}
	defer done()

	entries := a.ListVKHashes()
	targets := make([]VKHashEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.InPool || containsString(entry.UsedBy, profileName) {
			targets = append(targets, entry)
		}
	}

	total := len(targets)
	started := time.Now()
	a.onBridgeEvent("vk-hash-bulk-started", map[string]any{
		"operationId": operationID, "profileName": profileName, "total": total,
	})
	a.emitDiagnostic(diagnosticEvent{
		Subsystem: "HASH", OperationID: operationID, Server: profileName,
		Stage: "bulk", Action: "start", Message: fmt.Sprintf("Bulk hash check started: %d targets", total),
	})
	if total == 0 {
		a.onBridgeEvent("vk-hash-bulk-completed", map[string]any{
			"operationId": operationID, "profileName": profileName, "completed": 0,
			"total": 0, "state": "completed", "elapsedMs": int64(0),
		})
		return []VKHashCheckResult{}, nil
	}

	type checkEnvelope struct {
		result VKHashCheckResult
		err    error
		hashID string
	}
	jobs := make(chan VKHashEntry)
	out := make(chan checkEnvelope, total)
	workers := vkHashBulkConcurrency
	if workers > total {
		workers = total
	}

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for entry := range jobs {
				if ctx.Err() != nil {
					return
				}
				result, checkErr := a.checkVKHashCoordinated(ctx, entry.ID, profileName, operationID)
				out <- checkEnvelope{result: result, err: checkErr, hashID: entry.ID}
			}
		}()
	}

	go func() {
		defer close(jobs)
		for _, entry := range targets {
			select {
			case jobs <- entry:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		wg.Wait()
		close(out)
	}()

	results := make([]VKHashCheckResult, 0, total)
	completed := 0
	var firstErr error
	for item := range out {
		if item.err != nil {
			if !errors.Is(item.err, context.Canceled) && !errors.Is(item.err, context.DeadlineExceeded) && firstErr == nil {
				firstErr = item.err
			}
		} else if item.result.Status != "canceled" {
			results = append(results, item.result)
		}
		completed++
		a.onBridgeEvent("vk-hash-bulk-progress", map[string]any{
			"operationId": operationID, "profileName": profileName,
			"completed": completed, "total": total, "hashId": item.hashID,
			"status": item.result.Status, "elapsedMs": time.Since(started).Milliseconds(),
		})
	}

	state := "completed"
	if ctx.Err() != nil {
		state = "canceled"
	} else if firstErr != nil {
		state = "error"
	}
	a.onBridgeEvent("vk-hash-bulk-completed", map[string]any{
		"operationId": operationID, "profileName": profileName,
		"completed": completed, "total": total, "state": state,
		"elapsedMs": time.Since(started).Milliseconds(),
	})
	a.emitDiagnostic(diagnosticEvent{
		Subsystem: "HASH", OperationID: operationID, Server: profileName,
		Stage: "bulk", Action: "complete", Result: state,
		DurationMs: time.Since(started).Milliseconds(),
	})

	if ctx.Err() != nil {
		return results, nil
	}
	return results, firstErr
}

func (a *App) checkVKHashCoordinated(ctx context.Context, id, profileName, operationID string) (VKHashCheckResult, error) {
	profileName = sanitizeProfileName(profileName)
	if profileName == "" {
		return VKHashCheckResult{}, errors.New("выберите сервер для проверки VK-хеша")
	}
	if a.hashOps == nil {
		a.hashOps = newVKHashOperationState()
	}
	return a.hashOps.run(ctx, vkHashProbeKey(id, profileName), func(probeCtx context.Context) (VKHashCheckResult, error) {
		return a.checkVKHashProbe(probeCtx, id, profileName, operationID)
	})
}

func (a *App) checkVKHashProbe(ctx context.Context, id, profileName, operationID string) (VKHashCheckResult, error) {
	if a.bridge != nil && a.bridge.IsRunning() {
		return VKHashCheckResult{}, errors.New("проверка VK-хешей недоступна во время активного подключения")
	}

	vkHashRegistryMu.Lock()
	registry, err := a.store.loadVKHashRegistry()
	if err != nil {
		vkHashRegistryMu.Unlock()
		return VKHashCheckResult{}, err
	}
	index := findVKHashByID(&registry, id)
	if index < 0 {
		vkHashRegistryMu.Unlock()
		return VKHashCheckResult{}, errors.New("VK-хеш не найден")
	}
	entry := registry.Entries[index]
	vkHashRegistryMu.Unlock()

	profile, err := a.store.LoadProfile(profileName)
	if err != nil {
		return VKHashCheckResult{}, err
	}
	settings := a.store.LoadSettings()
	started := time.Now()

	a.onBridgeEvent("vk-hash-check-started", map[string]any{
		"operationId": operationID, "hashId": id, "profileName": profileName,
		"stage": "queued", "elapsedMs": int64(0),
	})
	a.emitDiagnostic(diagnosticEvent{
		Subsystem: "HASH", OperationID: operationID, HashID: id, Server: profileName,
		Stage: "probe", Action: "start",
	})

	probeCtx, cancel := context.WithTimeout(ctx, vkHashProbeTimeout)
	defer cancel()
	probe := core.ProbeHash(probeCtx, core.HashProbeConfig{
		PeerAddr: profile.PeerAddr,
		Password: profile.Password,
		Hash:     entry.Hash,
		DeviceID: profile.DeviceID,
		TurnHost: profile.TurnHost,
		TurnPort: profile.TurnPort,
		ObfsMode: settings.ObfsMode,
		OnProgress: func(progress core.HashProbeProgress) {
			a.onBridgeEvent("vk-hash-check-progress", map[string]any{
				"operationId": operationID, "hashId": id, "profileName": profileName,
				"stage": progress.Stage, "state": progress.State, "message": progress.Message,
				"elapsedMs": progress.ElapsedMs, "attempt": progress.Attempt,
			})
			a.emitDiagnostic(diagnosticEvent{
				Subsystem: "HASH", OperationID: operationID, HashID: id, Server: profileName,
				Stage: progress.Stage, Action: progress.State, Attempt: progress.Attempt,
				DurationMs: progress.ElapsedMs, Message: progress.Message,
			})
		},
	})

	result := VKHashCheckResult{
		HashID: id, Hash: entry.Hash, ProfileName: profileName,
		Status: string(probe.Status), CheckedAt: time.Now().UnixMilli(),
		ErrorType: string(probe.ErrorType), Message: probe.Message, LatencyMs: probe.LatencyMs,
	}

	if probe.ErrorType == core.HashProbeErrorCanceled {
		result.Status = "canceled"
		a.onBridgeEvent("vk-hash-check-result", map[string]any{
			"operationId": operationID, "hashId": id, "profileName": profileName,
			"status": result.Status, "errorType": result.ErrorType,
			"latencyMs": result.LatencyMs, "elapsedMs": time.Since(started).Milliseconds(),
		})
		return result, nil
	}

	check := VKHashCheck{
		Status: result.Status, CheckedAt: result.CheckedAt,
		ErrorType: result.ErrorType, Message: result.Message, LatencyMs: result.LatencyMs,
	}
	if check.Status == "" {
		check.Status = vkHashStatusError
		result.Status = check.Status
	}

	vkHashRegistryMu.Lock()
	registry, err = a.store.loadVKHashRegistry()
	if err == nil {
		index = findVKHashByID(&registry, id)
		if index >= 0 {
			if registry.Entries[index].Checks == nil {
				registry.Entries[index].Checks = make(map[string]VKHashCheck)
			}
			registry.Entries[index].Checks[profileName] = check
			err = a.store.saveVKHashRegistry(registry)
		}
	}
	vkHashRegistryMu.Unlock()
	if err != nil {
		return VKHashCheckResult{}, err
	}

	a.onBridgeEvent("vk-hash-check-result", map[string]any{
		"operationId": operationID, "hashId": id, "profileName": profileName,
		"status": result.Status, "errorType": result.ErrorType,
		"latencyMs": result.LatencyMs, "elapsedMs": time.Since(started).Milliseconds(),
	})
	a.emitDiagnostic(diagnosticEvent{
		Subsystem: "HASH", OperationID: operationID, HashID: id, Server: profileName,
		Stage: "probe", Action: "complete", Result: result.Status,
		DurationMs: time.Since(started).Milliseconds(), Message: result.ErrorType,
	})
	return result, nil
}

func (a *App) ReplaceVKHash(id, profileName string) (VKHashEntry, error) {
	profileName = sanitizeProfileName(profileName)
	if profileName == "" {
		return VKHashEntry{}, errors.New("выберите сервер для замены VK-хеша")
	}
	entries := a.ListVKHashes()
	var old VKHashEntry
	found := false
	existing := make([]string, 0, len(entries))
	for _, entry := range entries {
		existing = append(existing, entry.Hash)
		if entry.ID == id {
			old, found = entry, true
		}
	}
	if !found {
		return VKHashEntry{}, errors.New("VK-хеш не найден")
	}
	a.onBridgeEvent("vk-hash-replacement-started", map[string]any{"hashId": id, "profileName": profileName})

	generated, err := a.GenerateVKHashes(1, existing)
	if err != nil {
		return VKHashEntry{}, err
	}
	if len(generated) != 1 {
		return VKHashEntry{}, errors.New("VK не вернул новый хеш")
	}

	vkHashRegistryMu.Lock()
	registry, err := a.store.loadVKHashRegistry()
	if err != nil {
		vkHashRegistryMu.Unlock()
		return VKHashEntry{}, err
	}
	newEntry, _, err := ensureVKHashEntry(&registry, generated[0], "generated", false)
	if err == nil {
		err = a.store.saveVKHashRegistry(registry)
	}
	vkHashRegistryMu.Unlock()
	if err != nil {
		return VKHashEntry{}, err
	}

	checked, err := a.CheckVKHash(newEntry.ID, profileName)
	if err != nil || checked.Status != vkHashStatusValid {
		a.cleanupOrphanVKHash(newEntry.ID)
		if err != nil {
			return VKHashEntry{}, err
		}
		return VKHashEntry{}, fmt.Errorf("новый VK-хеш не прошёл проверку: %s", checked.Message)
	}

	profile, profileErr := a.store.LoadProfile(profileName)
	localReplaced := false
	if profileErr == nil {
		for i, raw := range profile.Hashes {
			hash, normalizeErr := normalizeVKCallHash(raw)
			if normalizeErr == nil && hash == old.Hash {
				profile.Hashes[i] = newEntry.Hash
				localReplaced = true
			}
		}
		if localReplaced {
			if err := a.store.SaveProfile(profileName, *profile); err != nil {
				return VKHashEntry{}, err
			}
		}
	}

	vkHashRegistryMu.Lock()
	registry, err = a.store.loadVKHashRegistry()
	if err != nil {
		vkHashRegistryMu.Unlock()
		return VKHashEntry{}, err
	}
	oldIndex := findVKHashByID(&registry, old.ID)
	newIndex := findVKHashByID(&registry, newEntry.ID)
	if oldIndex < 0 || newIndex < 0 {
		vkHashRegistryMu.Unlock()
		return VKHashEntry{}, errors.New("VK hash registry changed during replacement")
	}
	poolReplaced := registry.Entries[oldIndex].InPool
	if poolReplaced {
		registry.Entries[oldIndex].InPool = false
		registry.Entries[newIndex].InPool = true
	}
	if !registry.Entries[oldIndex].InPool &&
		len(profilesUsingVKHash(registry.Entries[oldIndex], a.store.ListProfiles())) == 0 {
		registry.Entries = append(registry.Entries[:oldIndex], registry.Entries[oldIndex+1:]...)
	}
	err = a.store.saveVKHashRegistry(registry)
	vkHashRegistryMu.Unlock()
	if err != nil {
		return VKHashEntry{}, err
	}
	if !localReplaced && !poolReplaced {
		return VKHashEntry{}, errors.New("VK-хеш не используется выбранным сервером и не входит в общий пул")
	}

	scope := "local"
	if localReplaced && poolReplaced {
		scope = "local+pool"
	} else if poolReplaced {
		scope = "pool"
	}
	a.onBridgeEvent("vk-hash-replaced", map[string]any{
		"profileName": profileName, "oldHash": old.Hash, "newHash": newEntry.Hash, "scope": scope,
	})
	a.onBridgeEvent("vk-hash-replacement-completed", map[string]any{
		"oldHashId": old.ID, "newHashId": newEntry.ID, "profileName": profileName, "scope": scope,
	})
	return a.vkHashEntry(newEntry.ID)
}

func (a *App) cleanupOrphanVKHash(id string) {
	vkHashRegistryMu.Lock()
	defer vkHashRegistryMu.Unlock()
	registry, err := a.store.loadVKHashRegistry()
	if err != nil {
		return
	}
	index := findVKHashByID(&registry, id)
	if index < 0 || registry.Entries[index].InPool {
		return
	}
	if len(profilesUsingVKHash(registry.Entries[index], a.store.ListProfiles())) != 0 {
		return
	}
	registry.Entries = append(registry.Entries[:index], registry.Entries[index+1:]...)
	_ = a.store.saveVKHashRegistry(registry)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func isVKHashCheckFresh(check VKHashCheck, now time.Time) bool {
	if check.CheckedAt <= 0 {
		return false
	}
	checkedAt := time.UnixMilli(check.CheckedAt)
	if checkedAt.After(now) {
		return false
	}
	ttl := vkHashCheckTTL
	if check.Status == vkHashStatusError {
		ttl = vkHashErrorTTL
	}
	return now.Sub(checkedAt) < ttl
}

func selectVKHashCandidates(local, pool []VKHashEntry, mode string) []VKHashEntry {
	mode = normalizeVKHashMode(mode)
	result := make([]VKHashEntry, 0, len(local)+len(pool))
	seen := make(map[string]struct{}, len(local)+len(pool))
	appendUnique := func(entries []VKHashEntry) {
		for _, entry := range entries {
			if _, ok := seen[entry.Hash]; ok {
				continue
			}
			seen[entry.Hash] = struct{}{}
			result = append(result, entry)
		}
	}
	if mode != vkHashModePool {
		appendUnique(local)
	}
	if mode != vkHashModeLocal {
		appendUnique(pool)
	}
	return result
}

func (a *App) candidateVKHashes(localHashes []string, mode string) ([]VKHashEntry, error) {
	vkHashRegistryMu.Lock()
	defer vkHashRegistryMu.Unlock()
	registry, err := a.store.loadVKHashRegistry()
	if err != nil {
		return nil, err
	}
	local := make([]VKHashEntry, 0, len(localHashes))
	changed := false
	for _, raw := range localHashes {
		entry, created, err := ensureVKHashEntry(&registry, raw, "manual", false)
		if err != nil {
			return nil, err
		}
		local = append(local, entry)
		changed = changed || created
	}
	pool := make([]VKHashEntry, 0)
	for _, entry := range registry.Entries {
		if entry.InPool {
			pool = append(pool, entry)
		}
	}
	if changed {
		if err := a.store.saveVKHashRegistry(registry); err != nil {
			return nil, err
		}
	}
	return selectVKHashCandidates(local, pool, mode), nil
}

func (a *App) storedVKHashCheck(id, profileName string) (VKHashCheck, bool) {
	vkHashRegistryMu.Lock()
	defer vkHashRegistryMu.Unlock()
	registry, err := a.store.loadVKHashRegistry()
	if err != nil {
		return VKHashCheck{}, false
	}
	index := findVKHashByID(&registry, id)
	if index < 0 {
		return VKHashCheck{}, false
	}
	check, ok := registry.Entries[index].Checks[profileName]
	return check, ok
}

func (a *App) prepareVKHashes(parent context.Context, params ConnectParams, operationID string) ([]string, error) {
	if parent == nil {
		parent = a.appContext()
	}
	localHashes, err := normalizeVKHashList(params.Hashes)
	if err != nil {
		return nil, err
	}
	policy := hashPolicyFromConnect(params)
	if strings.TrimSpace(params.ProfileName) != "" {
		if err := a.SyncProfileVKSettings(
			params.ProfileName, params.PeerAddr, params.Password, localHashes,
			policy.Mode, policy.AutoCheck, policy.AutoReplace,
		); err != nil {
			return nil, err
		}
	}

	candidates, err := a.candidateVKHashes(localHashes, policy.Mode)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, errors.New("нет VK-хешей для выбранной политики сервера")
	}
	if !policy.AutoCheck || strings.TrimSpace(params.ProfileName) == "" {
		return hashesFromEntries(candidates), nil
	}

	now := time.Now()
	freshValid := make([]string, 0, len(candidates))
	fallback := make([]VKHashEntry, 0, len(candidates))
	stale := make([]VKHashEntry, 0, len(candidates))
	confirmedInvalid := make(map[string]bool)

	for _, candidate := range candidates {
		check, ok := a.storedVKHashCheck(candidate.ID, params.ProfileName)
		if !ok || !isVKHashCheckFresh(check, now) {
			stale = append(stale, candidate)
			fallback = append(fallback, candidate)
			continue
		}

		switch check.Status {
		case vkHashStatusValid:
			freshValid = append(freshValid, candidate.Hash)
			fallback = append(fallback, candidate)
		case vkHashStatusError:
			fallback = append(fallback, candidate)
		case vkHashStatusInvalid:
			confirmedInvalid[candidate.ID] = true
		default:
			stale = append(stale, candidate)
			fallback = append(fallback, candidate)
		}
	}

	if len(freshValid) > 0 {
		a.emitDiagnostic(diagnosticEvent{
			Subsystem: "HASH", OperationID: operationID, Server: params.ProfileName,
			Stage: "preflight", Action: "cache-hit", Result: "valid",
			Message: fmt.Sprintf("Using %d fresh valid hash(es)", len(freshValid)),
		})
		return uniqueVKHashes(freshValid), nil
	}

	for _, candidate := range fallback {
		if check, ok := a.storedVKHashCheck(candidate.ID, params.ProfileName); ok &&
			isVKHashCheckFresh(check, now) && check.Status == vkHashStatusError {
			a.emitDiagnostic(diagnosticEvent{
				Subsystem: "HASH", OperationID: operationID, HashID: candidate.ID,
				Server: params.ProfileName, Stage: "preflight", Action: "cache-hit",
				Result: "fail-open", Message: "Recent infrastructure error; reusing hash without another probe",
			})
			return hashesFromEntries(fallback), nil
		}
	}

	preflightCtx, cancel := context.WithTimeout(parent, vkHashPreflightTimeout)
	defer cancel()
	started := time.Now()

	for _, candidate := range stale {
		if preflightCtx.Err() != nil {
			break
		}
		checked, checkErr := a.checkVKHashCoordinated(preflightCtx, candidate.ID, params.ProfileName, operationID)
		if checkErr != nil {
			if errors.Is(checkErr, context.Canceled) || errors.Is(checkErr, context.DeadlineExceeded) {
				break
			}
			return nil, checkErr
		}

		switch checked.Status {
		case vkHashStatusValid:
			a.emitDiagnostic(diagnosticEvent{
				Subsystem: "HASH", OperationID: operationID, HashID: candidate.ID,
				Server: params.ProfileName, Stage: "preflight", Action: "complete",
				Result: "valid", DurationMs: time.Since(started).Milliseconds(),
			})
			return []string{candidate.Hash}, nil
		case vkHashStatusError:
			usableNow := make([]VKHashEntry, 0, len(fallback))
			for _, fallbackCandidate := range fallback {
				if !confirmedInvalid[fallbackCandidate.ID] {
					usableNow = append(usableNow, fallbackCandidate)
				}
			}
			a.emitDiagnostic(diagnosticEvent{
				Subsystem: "HASH", OperationID: operationID, HashID: candidate.ID,
				Server: params.ProfileName, Stage: "preflight", Action: "complete",
				Result: "fail-open", DurationMs: time.Since(started).Milliseconds(),
				Message: checked.ErrorType,
			})
			return hashesFromEntries(usableNow), nil
		case vkHashStatusInvalid:
			confirmedInvalid[candidate.ID] = true
		case "canceled":
			break
		}
	}

	if parent.Err() != nil {
		return nil, fmt.Errorf("подключение отменено: %w", parent.Err())
	}

	usable := make([]VKHashEntry, 0, len(fallback))
	for _, candidate := range fallback {
		if !confirmedInvalid[candidate.ID] {
			usable = append(usable, candidate)
		}
	}
	if len(usable) > 0 {
		a.emitDiagnostic(diagnosticEvent{
			Subsystem: "HASH", OperationID: operationID, Server: params.ProfileName,
			Stage: "preflight", Action: "deadline", Result: "fail-open",
			DurationMs: time.Since(started).Milliseconds(),
			Message: fmt.Sprintf("Preflight deadline reached; using %d non-invalid hash(es)", len(usable)),
		})
		return hashesFromEntries(usable), nil
	}

	var lastReplacementErr error
	if policy.AutoReplace {
		replacementAttempts := 0
		for _, candidate := range candidates {
			if !confirmedInvalid[candidate.ID] || replacementAttempts >= maxAutoReplacements {
				continue
			}
			replacementAttempts++
			newEntry, replaceErr := a.ReplaceVKHash(candidate.ID, params.ProfileName)
			if replaceErr == nil {
				return []string{newEntry.Hash}, nil
			}
			lastReplacementErr = replaceErr
		}
	}
	if lastReplacementErr != nil {
		return nil, fmt.Errorf("рабочих VK-хешей не осталось; автозамена не удалась: %w", lastReplacementErr)
	}
	return nil, errors.New("рабочих VK-хешей не осталось")
}

func hashesFromEntries(entries []VKHashEntry) []string {
	result := make([]string, 0, len(entries))
	for _, entry := range entries {
		result = append(result, entry.Hash)
	}
	return uniqueVKHashes(result)
}

func uniqueVKHashes(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
