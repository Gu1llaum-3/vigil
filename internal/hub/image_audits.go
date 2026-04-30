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
	"github.com/Gu1llaum-3/vigil/internal/hub/notifications"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

const (
	containerImageAuditsCollection = "container_image_audits"
	containerImageAuditCronJobID   = "vigilContainerImageAudit"
	containerImageAuditCronExpr    = "0 3,15 * * *"

	imageAuditPolicyDigestLatest = "digest_latest"
	imageAuditPolicySemverMajor  = "semver_major"
	imageAuditPolicySemverMinor  = "semver_minor"
	imageAuditPolicyUnsupported  = "unsupported"

	imageAuditStatusUpToDate        = "up_to_date"
	imageAuditStatusUpdateAvailable = "update_available"
	imageAuditStatusUnknown         = "unknown"
	imageAuditStatusUnsupported     = "unsupported"
	imageAuditStatusCheckFailed     = "check_failed"
	imageAuditStatusDisabled        = "disabled"

	imageAuditLineStatusPatchAvailable = "patch_available"
	imageAuditLineStatusMinorAvailable = "minor_available"
	imageAuditLineStatusTagRebuilt     = "tag_rebuilt"

	containerAuditOverridesCollection = "container_audit_overrides"
	auditOverrideDigest               = "digest"
	auditOverridePatch                = "patch"
	auditOverrideMinor                = "minor"
	auditOverrideDisabled             = "disabled"
)

type ContainerImageAudit struct {
	Status         string `json:"status"`
	Policy         string `json:"policy"`
	Registry       string `json:"registry"`
	Repository     string `json:"repository"`
	Tag            string `json:"tag"`
	CurrentRef     string `json:"current_ref"`
	LocalImageID   string `json:"local_image_id"`
	LocalDigest    string `json:"local_digest"`
	LatestImageID  string `json:"latest_image_id"`
	LatestTag      string `json:"latest_tag"`
	LatestDigest   string `json:"latest_digest"`
	LineStatus     string `json:"line_status,omitempty"`
	LineLatestTag  string `json:"line_latest_tag,omitempty"`
	SameMajorTag   string `json:"same_major_latest_tag,omitempty"`
	OverallTag     string `json:"overall_latest_tag,omitempty"`
	NewMajorTag    string `json:"new_major_tag,omitempty"`
	HasMajorUpdate bool   `json:"major_update_available,omitempty"`
	CheckedAt      string `json:"checked_at"`
	Error          string `json:"error,omitempty"`
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
	Target         imageAuditTarget
	Status         string
	LineStatus     string
	LatestImageID  string
	LatestTag      string
	LatestDigest   string
	LineLatestTag  string
	SameMajorTag   string
	OverallTag     string
	NewMajorTag    string
	HasMajorUpdate bool
	CheckedAt      time.Time
	Error          string
}

type imageNotificationTarget struct {
	Scope string
	Tag   string
}

type numericVersion struct {
	Major   int
	Minor   int
	Patch   int
	Parts   int
	Variant string
}

func (h *Hub) collectContainerImageAuditResults(ctx context.Context, registryClient imageRegistryClient) ([]imageAuditResult, int, error) {
	snapshotRecords, err := h.FindAllRecords("host_snapshots")
	if err != nil {
		return nil, 0, err
	}

	overrides, err := h.loadContainerAuditOverrides()
	if err != nil {
		slog.Warn("container image audits: failed to load overrides", "err", err)
		overrides = nil
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

			target, earlyResult := buildImageAuditTarget(agentID, snapshot.Architecture, container)

			if override, ok := overrides[auditOverrideKey(agentID, container.Name)]; ok {
				if override == auditOverrideDisabled {
					results = append(results, imageAuditResult{
						Target:    target,
						Status:    imageAuditStatusDisabled,
						CheckedAt: time.Now().UTC(),
					})
					continue
				}
				if mapped := mapOverrideToInternalPolicy(override); mapped != "" {
					target.Policy = mapped
					earlyResult = nil
				}
			}

			if earlyResult != nil {
				results = append(results, *earlyResult)
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

func auditOverrideKey(agentID, containerName string) string {
	return agentID + "|" + containerName
}

func mapOverrideToInternalPolicy(override string) string {
	switch override {
	case auditOverrideDigest:
		return imageAuditPolicyDigestLatest
	case auditOverridePatch:
		return imageAuditPolicySemverMinor
	case auditOverrideMinor:
		return imageAuditPolicySemverMajor
	default:
		return ""
	}
}

func (h *Hub) loadContainerAuditOverrides() (map[string]string, error) {
	records, err := h.FindAllRecords(containerAuditOverridesCollection)
	if err != nil {
		// Collection may not exist on legacy installs; treat as empty.
		if strings.Contains(strings.ToLower(err.Error()), containerAuditOverridesCollection) {
			return nil, nil
		}
		return nil, err
	}
	overrides := make(map[string]string, len(records))
	for _, rec := range records {
		overrides[auditOverrideKey(rec.GetString("agent"), rec.GetString("container_name"))] = rec.GetString("policy")
	}
	return overrides, nil
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
			result.LineStatus = imageAuditStatusCheckFailed
			result.Error = err.Error()
			return result
		}
		result.LatestTag = target.Tag
		result.LineLatestTag = target.Tag
		result.SameMajorTag = target.Tag
		result.OverallTag = target.Tag
		result.LatestDigest = remoteDigest
		result.LatestImageID = remoteDigest
		if target.LocalImageID == "" {
			result.Status = imageAuditStatusUnknown
			result.LineStatus = imageAuditStatusUnknown
			return result
		}
		if target.LocalImageID == remoteDigest {
			result.Status = imageAuditStatusUpToDate
			result.LineStatus = imageAuditStatusUpToDate
		} else {
			result.Status = imageAuditStatusUpdateAvailable
			result.LineStatus = imageAuditStatusUpdateAvailable
		}
		return result
	case imageAuditPolicySemverMajor, imageAuditPolicySemverMinor:
		currentVersion, ok := parseNumericVersion(target.Tag)
		if !ok {
			result.Status = imageAuditStatusUnsupported
			result.LineStatus = imageAuditStatusUnsupported
			return result
		}
		tags, err := registryClient.ListTags(ctx, fmt.Sprintf("%s/%s", target.Registry, target.Repository))
		if err != nil {
			result.Status = imageAuditStatusCheckFailed
			result.LineStatus = imageAuditStatusCheckFailed
			result.Error = err.Error()
			return result
		}
		lineTag, foundLine := selectAuditTag(tags, target.Tag, currentVersion, target.Policy)
		sameMajorTag, _ := selectAuditTag(tags, target.Tag, currentVersion, imageAuditPolicySemverMajor)
		overallTag, overallVersion, foundOverall := selectLatestSemverTag(tags, currentVersion.Variant)
		if !foundOverall {
			overallTag = target.Tag
		}

		result.LatestTag = lineTag
		result.LineLatestTag = lineTag
		result.SameMajorTag = sameMajorTag
		result.OverallTag = overallTag
		if currentVersion.Parts < 3 {
			remoteDigest, err := registryClient.ResolvedDigest(ctx, target.CurrentRef, target.Architecture)
			if err != nil {
				result.Status = imageAuditStatusCheckFailed
				result.LineStatus = imageAuditStatusCheckFailed
				result.Error = err.Error()
				return result
			}
			result.LatestDigest = remoteDigest
			result.LatestImageID = remoteDigest
			if target.LocalImageID == "" {
				result.Status = imageAuditStatusUnknown
				result.LineStatus = imageAuditStatusUnknown
			} else if target.LocalImageID == remoteDigest {
				result.Status = imageAuditStatusUpToDate
				result.LineStatus = imageAuditStatusUpToDate
			} else {
				result.Status = imageAuditStatusUpdateAvailable
				if target.Policy == imageAuditPolicySemverMajor {
					result.LineStatus = imageAuditLineStatusMinorAvailable
				} else {
					result.LineStatus = imageAuditLineStatusPatchAvailable
				}
			}
		} else if foundLine {
			result.Status = imageAuditStatusUpdateAvailable
			result.LineStatus = imageAuditLineStatusPatchAvailable
		} else {
			remoteDigest, err := registryClient.ResolvedDigest(ctx, target.CurrentRef, target.Architecture)
			if err != nil {
				result.Status = imageAuditStatusCheckFailed
				result.LineStatus = imageAuditStatusCheckFailed
				result.Error = err.Error()
				return result
			}
			result.LatestDigest = remoteDigest
			result.LatestImageID = remoteDigest
			if target.LocalImageID == "" {
				result.Status = imageAuditStatusUnknown
				result.LineStatus = imageAuditStatusUnknown
			} else if target.LocalImageID == remoteDigest {
				result.Status = imageAuditStatusUpToDate
				result.LineStatus = imageAuditStatusUpToDate
			} else {
				result.Status = imageAuditStatusUpdateAvailable
				result.LineStatus = imageAuditLineStatusTagRebuilt
			}
		}
		if foundOverall && overallVersion.Major > currentVersion.Major {
			result.HasMajorUpdate = true
			result.NewMajorTag = overallTag
		}
		candidateDigest, err := registryClient.HeadDigest(ctx, fmt.Sprintf("%s/%s:%s", target.Registry, target.Repository, lineTag))
		if err == nil {
			result.LatestDigest = candidateDigest
			if result.LatestImageID == "" {
				result.LatestImageID = candidateDigest
			}
		}
		return result
	default:
		result.Status = imageAuditStatusUnsupported
		result.LineStatus = imageAuditStatusUnsupported
		return result
	}
}

func (h *Hub) runContainerImageAudit() (map[string]any, error) {
	client := remoteImageRegistryClient{keychain: h.registryKeychain()}
	results, seenCount, err := h.collectContainerImageAuditResults(context.Background(), client)
	if err != nil {
		return nil, err
	}

	counts := map[string]int{}
	notificationsEmitted := 0
	notificationExamples := make([]map[string]any, 0, 5)
	notificationOccurredAt := time.Now().UTC()
	for _, result := range results {
		counts[result.Status]++
		notified, err := h.upsertContainerImageAudit(result)
		if err != nil {
			return nil, err
		}
		if notified {
			notificationsEmitted++
			notificationOccurredAt = result.CheckedAt.UTC()
			if len(notificationExamples) < 5 {
				notificationExamples = append(notificationExamples, map[string]any{
					"agent_id":       result.Target.AgentID,
					"container_id":   result.Target.ContainerID,
					"container_name": result.Target.ContainerName,
					"image_ref":      result.Target.CurrentRef,
					"latest_tag":     firstNonEmpty(result.LineLatestTag, result.SameMajorTag, result.NewMajorTag),
				})
			}
		}
	}
	if err := h.createContainerImageSystemNotification(notificationsEmitted, notificationOccurredAt, notificationExamples); err != nil {
		slog.Warn("container image audits: failed to create system notification", "err", err)
	}

	removed, err := h.deleteStaleContainerImageAudits(results)
	if err != nil {
		return nil, err
	}

	payload := map[string]any{
		"checked_containers":    seenCount,
		"audited_records":       len(results),
		"removed_records":       removed,
		"notifications_emitted": notificationsEmitted,
		"status_counts":         counts,
	}
	return payload, nil
}

func (h *Hub) upsertContainerImageAudit(result imageAuditResult) (bool, error) {
	rec, err := h.FindFirstRecordByFilter(
		containerImageAuditsCollection,
		"agent = {:agent} && container_id = {:container_id}",
		dbx.Params{"agent": result.Target.AgentID, "container_id": result.Target.ContainerID},
	)
	created := false
	if err != nil {
		collection, colErr := h.FindCachedCollectionByNameOrId(containerImageAuditsCollection)
		if colErr != nil {
			return false, colErr
		}
		rec = core.NewRecord(collection)
		rec.Set("agent", result.Target.AgentID)
		rec.Set("container_id", result.Target.ContainerID)
		created = true
	}

	prevSignature := rec.GetString("last_notified_signature")

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
		"current_ref":            result.Target.CurrentRef,
		"local_image_id":         result.Target.LocalImageID,
		"local_digest":           result.Target.LocalDigest,
		"latest_image_id":        result.LatestImageID,
		"latest_tag":             result.LatestTag,
		"latest_digest":          result.LatestDigest,
		"line_status":            result.LineStatus,
		"line_latest_tag":        result.LineLatestTag,
		"same_major_latest_tag":  result.SameMajorTag,
		"overall_latest_tag":     result.OverallTag,
		"new_major_tag":          result.NewMajorTag,
		"major_update_available": result.HasMajorUpdate,
	})

	targets := buildImageNotificationTargets(result)
	signature := buildImageNotificationSignature(targets)
	notified := false
	if signature == "" {
		rec.Set("last_notified_signature", "")
		rec.Set("last_notified_at", "")
	} else if signature != prevSignature {
		h.dispatchContainerImageAuditNotification(rec, result, targets)
		notified = true
		rec.Set("last_notified_signature", signature)
		rec.Set("last_notified_at", result.CheckedAt.UTC().Format(time.RFC3339))
	} else if created && prevSignature == "" {
		rec.Set("last_notified_signature", prevSignature)
	}

	if err := h.SaveNoValidate(rec); err != nil {
		return false, err
	}
	return notified, nil
}

func buildImageNotificationTargets(result imageAuditResult) []imageNotificationTarget {
	targets := make([]imageNotificationTarget, 0, 3)
	seen := make(map[string]struct{}, 3)
	appendTarget := func(scope, tag string) {
		if tag == "" || tag == result.Target.Tag {
			return
		}
		key := scope + ":" + tag
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		targets = append(targets, imageNotificationTarget{Scope: scope, Tag: tag})
	}

	if result.LineLatestTag != "" && result.LineLatestTag != result.Target.Tag {
		switch result.LineStatus {
		case imageAuditLineStatusPatchAvailable, imageAuditLineStatusMinorAvailable:
			appendTarget("line", result.LineLatestTag)
		}
	}
	if result.SameMajorTag != "" && result.SameMajorTag != result.LineLatestTag && result.SameMajorTag != result.Target.Tag {
		appendTarget("same_major", result.SameMajorTag)
	}
	if result.NewMajorTag != "" {
		appendTarget("new_major", result.NewMajorTag)
	}

	return targets
}

func buildImageNotificationSignature(targets []imageNotificationTarget) string {
	if len(targets) == 0 {
		return ""
	}
	parts := make([]string, 0, len(targets))
	for _, target := range targets {
		parts = append(parts, target.Scope+":"+target.Tag)
	}
	return strings.Join(parts, "|")
}

func (h *Hub) dispatchContainerImageAuditNotification(rec *core.Record, result imageAuditResult, targets []imageNotificationTarget) {
	if len(targets) == 0 || h.notifier == nil {
		return
	}
	agentName := result.Target.AgentID
	if agentID := rec.GetString("agent"); agentID != "" {
		if agent, err := h.FindRecordById("agents", agentID); err == nil {
			agentName = firstNonEmpty(agent.GetString("name"), agent.Id)
		}
	}
	labels := make([]string, 0, len(targets))
	for _, target := range targets {
		switch target.Scope {
		case "line":
			labels = append(labels, fmt.Sprintf("patch line %s", target.Tag))
		case "same_major":
			labels = append(labels, fmt.Sprintf("same major %s", target.Tag))
		case "new_major":
			labels = append(labels, fmt.Sprintf("new major %s", target.Tag))
		}
	}

	h.notifier.Dispatch(notifications.Event{
		Kind:       notifications.EventContainerImageUpdateAvailable,
		OccurredAt: result.CheckedAt.UTC(),
		Resource: notifications.ResourceRef{
			ID:   auditContainerKey(result.Target.AgentID, result.Target.ContainerID),
			Name: firstNonEmpty(result.Target.ContainerName, result.Target.ContainerID),
			Type: "container_image",
		},
		Previous: result.Target.Tag,
		Current:  buildImageNotificationSignature(targets),
		Details: map[string]any{
			"agent_id":        result.Target.AgentID,
			"agent_name":      agentName,
			"container_id":    result.Target.ContainerID,
			"container_name":  firstNonEmpty(result.Target.ContainerName, result.Target.ContainerID),
			"current_ref":     result.Target.CurrentRef,
			"current_tag":     result.Target.Tag,
			"update_targets":  labels,
			"line_latest_tag": result.LineLatestTag,
			"same_major_tag":  result.SameMajorTag,
			"new_major_tag":   result.NewMajorTag,
		},
	})
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
	return registry, repository, tag, imageAuditPolicySemverMinor, nil
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
	variant := ""
	if dash := strings.Index(trimmed, "-"); dash > 0 {
		variant = trimmed[dash+1:]
		trimmed = trimmed[:dash]
	}
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
	return numericVersion{Major: values[0], Minor: values[1], Patch: values[2], Parts: len(parts), Variant: variant}, true
}

func selectAuditTag(tags []string, currentTag string, current numericVersion, policy string) (string, bool) {
	best := current
	bestTag := currentTag
	found := false
	for _, tag := range tags {
		candidate, ok := parseNumericVersion(tag)
		if !ok {
			continue
		}
		if candidate.Variant != current.Variant {
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
			found = true
		}
	}
	return bestTag, found
}

func selectLatestSemverTag(tags []string, variant string) (string, numericVersion, bool) {
	var best numericVersion
	bestTag := ""
	found := false
	for _, tag := range tags {
		candidate, ok := parseNumericVersion(tag)
		if !ok {
			continue
		}
		if candidate.Variant != variant {
			continue
		}
		if !found || compareNumericVersions(candidate, best) > 0 {
			best = candidate
			bestTag = tag
			found = true
		}
	}
	return bestTag, best, found
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
	tag := strings.Join(values, ".")
	if version.Variant != "" {
		tag += "-" + version.Variant
	}
	return tag
}

func containerImageAuditFromRecord(rec *core.Record) *ContainerImageAudit {
	status := rec.GetString("status")
	if status == "" {
		return nil
	}
	details := auditDetailsMap(rec.Get("details"))
	return &ContainerImageAudit{
		Status:         status,
		Policy:         rec.GetString("policy"),
		Registry:       rec.GetString("registry"),
		Repository:     rec.GetString("repository"),
		Tag:            rec.GetString("tag"),
		CurrentRef:     rec.GetString("image_ref"),
		LocalImageID:   rec.GetString("local_image_id"),
		LocalDigest:    rec.GetString("local_digest"),
		LatestImageID:  rec.GetString("latest_image_id"),
		LatestTag:      rec.GetString("latest_tag"),
		LatestDigest:   rec.GetString("latest_digest"),
		LineStatus:     stringDetail(details, "line_status"),
		LineLatestTag:  firstNonEmpty(stringDetail(details, "line_latest_tag"), rec.GetString("latest_tag")),
		SameMajorTag:   stringDetail(details, "same_major_latest_tag"),
		OverallTag:     stringDetail(details, "overall_latest_tag"),
		NewMajorTag:    stringDetail(details, "new_major_tag"),
		HasMajorUpdate: boolDetail(details, "major_update_available"),
		CheckedAt:      rec.GetString("checked_at"),
		Error:          rec.GetString("error"),
	}
}

func auditDetailsMap(raw any) map[string]any {
	if raw == nil {
		return nil
	}
	if details, ok := raw.(map[string]any); ok {
		return details
	}
	var payload []byte
	switch value := raw.(type) {
	case []byte:
		payload = value
	case types.JSONRaw:
		payload = []byte(value)
	case string:
		payload = []byte(value)
	default:
		return nil
	}
	if len(payload) == 0 {
		return nil
	}
	var details map[string]any
	if err := json.Unmarshal(payload, &details); err != nil {
		return nil
	}
	return details
}

func stringDetail(details map[string]any, key string) string {
	if details == nil {
		return ""
	}
	value, _ := details[key].(string)
	return value
}

func boolDetail(details map[string]any, key string) bool {
	if details == nil {
		return false
	}
	value, _ := details[key].(bool)
	return value
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
