package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Gu1llaum-3/vigil/internal/common"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	containerImageAuditsCollection = "container_image_audits"
	containerImageAuditCronJobID   = "vigilContainerImageAudit"
	containerImageAuditCronExpr    = "0 3 * * *"

	imageAuditPolicyDigestLatest = "digest_latest"
	imageAuditPolicySemverMajor  = "semver_major"
	imageAuditPolicySemverMinor  = "semver_minor"
	imageAuditPolicyUnsupported  = "unsupported"

	imageAuditStatusUpToDate        = "up_to_date"
	imageAuditStatusUpdateAvailable = "update_available"
	imageAuditStatusUnknown         = "unknown"
	imageAuditStatusUnsupported     = "unsupported"
	imageAuditStatusCheckFailed     = "check_failed"
)

type ContainerImageAudit struct {
	Status        string `json:"status"`
	Policy        string `json:"policy"`
	Registry      string `json:"registry"`
	Repository    string `json:"repository"`
	Tag           string `json:"tag"`
	CurrentRef    string `json:"current_ref"`
	LocalImageID  string `json:"local_image_id"`
	LocalDigest   string `json:"local_digest"`
	LatestImageID string `json:"latest_image_id"`
	LatestTag     string `json:"latest_tag"`
	LatestDigest  string `json:"latest_digest"`
	CheckedAt     string `json:"checked_at"`
	Error         string `json:"error,omitempty"`
}

type imageAuditTarget struct {
	AgentID           string
	ContainerID       string
	ContainerName     string
	Architecture      string
	CurrentRef        string
	LocalImageID      string
	LocalDigest       string
	CurrentRefImageID string
	CurrentRefDigest  string
	Registry          string
	Repository        string
	Tag               string
	Policy            string
}

type imageAuditResult struct {
	Target        imageAuditTarget
	Status        string
	LatestImageID string
	LatestTag     string
	LatestDigest  string
	CheckedAt     time.Time
	Error         string
}

type numericVersion struct {
	Major int
	Minor int
	Patch int
	Parts int
}

func (h *Hub) collectContainerImageAuditResults(ctx context.Context, registryClient imageRegistryClient) ([]imageAuditResult, int, error) {
	snapshotRecords, err := h.FindAllRecords("host_snapshots")
	if err != nil {
		return nil, 0, err
	}

	results := make([]imageAuditResult, 0)
	seenContainers := make(map[string]struct{})

	for _, rec := range snapshotRecords {
		agentID := rec.GetString("agent")
		var snapshot common.HostSnapshotResponse
		if err := json.Unmarshal([]byte(rec.GetString("data")), &snapshot); err != nil {
			slog.Warn("container image audits: failed to decode snapshot", "agent", agentID, "err", err)
			continue
		}
		if snapshot.Docker.State != "available" {
			continue
		}

		for _, container := range snapshot.Docker.Containers {
			seenContainers[auditContainerKey(agentID, container.ID)] = struct{}{}

			target, result := buildImageAuditTarget(agentID, snapshot.Architecture, container)
			if result != nil {
				results = append(results, *result)
				continue
			}

			auditCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			resolved := resolveImageAudit(auditCtx, registryClient, target)
			cancel()
			results = append(results, resolved)
		}
	}

	return results, len(seenContainers), nil
}

func buildImageAuditTarget(agentID, architecture string, container common.ContainerInfo) (imageAuditTarget, *imageAuditResult) {
	target := imageAuditTarget{
		AgentID:           agentID,
		ContainerID:       container.ID,
		ContainerName:     container.Name,
		Architecture:      architecture,
		CurrentRef:        firstNonEmpty(container.ImageRef, container.Image),
		LocalImageID:      container.ImageID,
		LocalDigest:       resolveLocalDigest(container),
		CurrentRefImageID: container.CurrentRefImageID,
		CurrentRefDigest:  resolveCurrentRefDigest(container),
	}

	registry, repository, tag, policy, err := normalizeImageAuditRef(target.CurrentRef)
	if err != nil {
		return target, &imageAuditResult{
			Target:    target,
			Status:    imageAuditStatusUnknown,
			CheckedAt: time.Now().UTC(),
			Error:     err.Error(),
		}
	}

	target.Registry = registry
	target.Repository = repository
	target.Tag = tag
	target.Policy = policy

	if policy == imageAuditPolicyUnsupported {
		return target, &imageAuditResult{
			Target:    target,
			Status:    imageAuditStatusUnsupported,
			CheckedAt: time.Now().UTC(),
		}
	}

	return target, nil
}

func resolveImageAudit(ctx context.Context, registryClient imageRegistryClient, target imageAuditTarget) imageAuditResult {
	result := imageAuditResult{Target: target, CheckedAt: time.Now().UTC()}

	switch target.Policy {
	case imageAuditPolicyDigestLatest:
		remoteDigest, err := registryClient.ResolvedDigest(ctx, target.CurrentRef, target.Architecture)
		if err != nil {
			result.Status = imageAuditStatusCheckFailed
			result.Error = err.Error()
			return result
		}
		result.LatestTag = target.Tag
		result.LatestDigest = remoteDigest
		result.LatestImageID = remoteDigest
		if target.LocalImageID == "" {
			result.Status = imageAuditStatusUnknown
			return result
		}
		if target.LocalImageID == remoteDigest {
			result.Status = imageAuditStatusUpToDate
		} else {
			result.Status = imageAuditStatusUpdateAvailable
		}
		return result
	case imageAuditPolicySemverMajor, imageAuditPolicySemverMinor:
		currentVersion, ok := parseNumericVersion(target.Tag)
		if !ok {
			result.Status = imageAuditStatusUnsupported
			return result
		}
		tags, err := registryClient.ListTags(ctx, fmt.Sprintf("%s/%s", target.Registry, target.Repository))
		if err != nil {
			result.Status = imageAuditStatusCheckFailed
			result.Error = err.Error()
			return result
		}
		candidateTag, found := selectAuditTag(tags, currentVersion, target.Policy)
		if !found {
			result.Status = imageAuditStatusUpToDate
			result.LatestTag = target.Tag
			return result
		}
		result.LatestTag = candidateTag
		if candidateTag != target.Tag {
			result.Status = imageAuditStatusUpdateAvailable
		} else {
			result.Status = imageAuditStatusUpToDate
		}
		candidateDigest, err := registryClient.HeadDigest(ctx, fmt.Sprintf("%s/%s:%s", target.Registry, target.Repository, candidateTag))
		if err == nil {
			result.LatestDigest = candidateDigest
		}
		return result
	default:
		result.Status = imageAuditStatusUnsupported
		return result
	}
}

func (h *Hub) runContainerImageAudit() (map[string]any, error) {
	results, seenCount, err := h.collectContainerImageAuditResults(context.Background(), remoteImageRegistryClient{})
	if err != nil {
		return nil, err
	}

	counts := map[string]int{}
	for _, result := range results {
		counts[result.Status]++
		if err := h.upsertContainerImageAudit(result); err != nil {
			return nil, err
		}
	}

	removed, err := h.deleteStaleContainerImageAudits(results)
	if err != nil {
		return nil, err
	}

	payload := map[string]any{
		"checked_containers": seenCount,
		"audited_records":    len(results),
		"removed_records":    removed,
		"status_counts":      counts,
	}
	return payload, nil
}

func (h *Hub) upsertContainerImageAudit(result imageAuditResult) error {
	rec, err := h.FindFirstRecordByFilter(
		containerImageAuditsCollection,
		"agent = {:agent} && container_id = {:container_id}",
		dbx.Params{"agent": result.Target.AgentID, "container_id": result.Target.ContainerID},
	)
	if err != nil {
		collection, colErr := h.FindCachedCollectionByNameOrId(containerImageAuditsCollection)
		if colErr != nil {
			return colErr
		}
		rec = core.NewRecord(collection)
		rec.Set("agent", result.Target.AgentID)
		rec.Set("container_id", result.Target.ContainerID)
	}

	rec.Set("container_name", result.Target.ContainerName)
	rec.Set("image_ref", result.Target.CurrentRef)
	rec.Set("registry", result.Target.Registry)
	rec.Set("repository", result.Target.Repository)
	rec.Set("tag", result.Target.Tag)
	rec.Set("local_image_id", result.Target.LocalImageID)
	rec.Set("local_digest", result.Target.LocalDigest)
	rec.Set("policy", result.Target.Policy)
	rec.Set("status", result.Status)
	rec.Set("latest_image_id", result.LatestImageID)
	rec.Set("latest_tag", result.LatestTag)
	rec.Set("latest_digest", result.LatestDigest)
	rec.Set("checked_at", result.CheckedAt.UTC().Format(time.RFC3339))
	rec.Set("error", result.Error)
	rec.Set("details", map[string]any{
		"current_ref":     result.Target.CurrentRef,
		"local_image_id":  result.Target.LocalImageID,
		"local_digest":    result.Target.LocalDigest,
		"latest_image_id": result.LatestImageID,
		"latest_tag":      result.LatestTag,
		"latest_digest":   result.LatestDigest,
	})
	return h.SaveNoValidate(rec)
}

func (h *Hub) deleteStaleContainerImageAudits(results []imageAuditResult) (int, error) {
	records, err := h.FindAllRecords(containerImageAuditsCollection)
	if err != nil {
		return 0, err
	}

	active := make(map[string]struct{}, len(results))
	for _, result := range results {
		active[auditContainerKey(result.Target.AgentID, result.Target.ContainerID)] = struct{}{}
	}

	deleted := 0
	for _, rec := range records {
		key := auditContainerKey(rec.GetString("agent"), rec.GetString("container_id"))
		if _, ok := active[key]; ok {
			continue
		}
		if err := h.Delete(rec); err != nil {
			return deleted, err
		}
		deleted++
	}

	return deleted, nil
}

func auditContainerKey(agentID, containerID string) string {
	return agentID + "|" + containerID
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func resolveLocalDigest(container common.ContainerInfo) string {
	for _, rawDigest := range container.RepoDigests {
		ref, err := name.NewDigest(rawDigest, name.WeakValidation)
		if err != nil {
			continue
		}
		registry, repository, _, _, err := normalizeImageAuditRef(firstNonEmpty(container.ImageRef, container.Image))
		if err != nil {
			continue
		}
		if normalizeRegistry(ref.Context().RegistryStr()) == registry && ref.Context().RepositoryStr() == repository {
			return ref.DigestStr()
		}
	}
	return ""
}

func resolveCurrentRefDigest(container common.ContainerInfo) string {
	for _, rawDigest := range container.CurrentRefRepoDigests {
		ref, err := name.NewDigest(rawDigest, name.WeakValidation)
		if err != nil {
			continue
		}
		registry, repository, _, _, err := normalizeImageAuditRef(firstNonEmpty(container.ImageRef, container.Image))
		if err != nil {
			continue
		}
		if normalizeRegistry(ref.Context().RegistryStr()) == registry && ref.Context().RepositoryStr() == repository {
			return ref.DigestStr()
		}
	}
	return ""
}

func normalizeImageAuditRef(raw string) (registry, repository, tag, policy string, err error) {
	ref, err := name.ParseReference(raw, name.WeakValidation)
	if err != nil {
		return "", "", "", "", err
	}

	tagRef, ok := ref.(name.Tag)
	if !ok {
		return normalizeRegistry(ref.Context().RegistryStr()), ref.Context().RepositoryStr(), "", imageAuditPolicyUnsupported, nil
	}

	registry = normalizeRegistry(tagRef.Context().RegistryStr())
	repository = tagRef.Context().RepositoryStr()
	tag = tagRef.TagStr()
	if tag == "" {
		tag = "latest"
	}

	if registry != "docker.io" && registry != "ghcr.io" {
		return registry, repository, tag, imageAuditPolicyUnsupported, nil
	}

	if tag == "latest" {
		return registry, repository, tag, imageAuditPolicyDigestLatest, nil
	}

	version, ok := parseNumericVersion(tag)
	if !ok {
		return registry, repository, tag, imageAuditPolicyUnsupported, nil
	}
	if version.Parts == 1 {
		return registry, repository, tag, imageAuditPolicySemverMajor, nil
	}
	if version.Parts == 2 {
		return registry, repository, tag, imageAuditPolicySemverMinor, nil
	}
	return registry, repository, tag, imageAuditPolicySemverMajor, nil
}

func normalizeRegistry(registry string) string {
	switch registry {
	case "index.docker.io", "docker.io":
		return "docker.io"
	default:
		return registry
	}
}

func normalizePlatformArchitecture(arch string) string {
	switch arch {
	case "aarch64":
		return "arm64"
	case "x86_64":
		return "amd64"
	default:
		if arch == "" {
			return "amd64"
		}
		return arch
	}
}

func parseNumericVersion(tag string) (numericVersion, bool) {
	trimmed := strings.TrimPrefix(tag, "v")
	parts := strings.Split(trimmed, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return numericVersion{}, false
	}
	values := [3]int{}
	for i, part := range parts {
		if part == "" {
			return numericVersion{}, false
		}
		value, err := strconv.Atoi(part)
		if err != nil {
			return numericVersion{}, false
		}
		values[i] = value
	}
	return numericVersion{Major: values[0], Minor: values[1], Patch: values[2], Parts: len(parts)}, true
}

func selectAuditTag(tags []string, current numericVersion, policy string) (string, bool) {
	best := current
	bestTag := ""
	for _, tag := range tags {
		candidate, ok := parseNumericVersion(tag)
		if !ok {
			continue
		}
		switch policy {
		case imageAuditPolicySemverMajor:
			if candidate.Major != current.Major {
				continue
			}
		case imageAuditPolicySemverMinor:
			if candidate.Major != current.Major || candidate.Minor != current.Minor {
				continue
			}
		default:
			continue
		}
		if compareNumericVersions(candidate, best) > 0 {
			best = candidate
			bestTag = tag
		}
	}
	if bestTag == "" {
		return versionToTag(current), false
	}
	return bestTag, true
}

func compareNumericVersions(left, right numericVersion) int {
	leftValues := []int{left.Major, left.Minor, left.Patch}
	rightValues := []int{right.Major, right.Minor, right.Patch}
	for i := range leftValues {
		if leftValues[i] > rightValues[i] {
			return 1
		}
		if leftValues[i] < rightValues[i] {
			return -1
		}
	}
	return 0
}

func versionToTag(version numericVersion) string {
	values := []string{strconv.Itoa(version.Major)}
	if version.Parts >= 2 {
		values = append(values, strconv.Itoa(version.Minor))
	}
	if version.Parts >= 3 {
		values = append(values, strconv.Itoa(version.Patch))
	}
	return strings.Join(values, ".")
}

func containerImageAuditFromRecord(rec *core.Record) *ContainerImageAudit {
	status := rec.GetString("status")
	if status == "" {
		return nil
	}
	return &ContainerImageAudit{
		Status:        status,
		Policy:        rec.GetString("policy"),
		Registry:      rec.GetString("registry"),
		Repository:    rec.GetString("repository"),
		Tag:           rec.GetString("tag"),
		CurrentRef:    rec.GetString("image_ref"),
		LocalImageID:  rec.GetString("local_image_id"),
		LocalDigest:   rec.GetString("local_digest"),
		LatestImageID: rec.GetString("latest_image_id"),
		LatestTag:     rec.GetString("latest_tag"),
		LatestDigest:  rec.GetString("latest_digest"),
		CheckedAt:     rec.GetString("checked_at"),
		Error:         rec.GetString("error"),
	}
}

func auditMap(records []*core.Record) map[string]*ContainerImageAudit {
	result := make(map[string]*ContainerImageAudit, len(records))
	for _, rec := range records {
		audit := containerImageAuditFromRecord(rec)
		if audit == nil {
			continue
		}
		result[auditContainerKey(rec.GetString("agent"), rec.GetString("container_id"))] = audit
	}
	return result
}

func loadContainerImageAuditRecords(h *Hub) ([]*core.Record, error) {
	records, err := h.FindAllRecords(containerImageAuditsCollection)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "container_image_audits") {
			return nil, nil
		}
		return nil, err
	}
	sort.SliceStable(records, func(i, j int) bool {
		return records[i].GetString("container_id") < records[j].GetString("container_id")
	})
	return records, nil
}
