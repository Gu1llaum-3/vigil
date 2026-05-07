package hub

import (
	"fmt"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/cron"
)

const scheduledJobsCollection = "scheduled_jobs"

type ScheduledJobDefinition struct {
	Key             string
	Label           string
	Description     string
	DefaultSchedule string
	Run             func() (map[string]any, error)
}

type ScheduledJobRecord struct {
	Key            string         `json:"key"`
	Label          string         `json:"label"`
	Description    string         `json:"description"`
	Schedule       string         `json:"schedule"`
	LastRunAt      string         `json:"last_run_at"`
	LastSuccessAt  string         `json:"last_success_at"`
	LastStatus     string         `json:"last_status"`
	LastError      string         `json:"last_error"`
	LastResult     map[string]any `json:"last_result,omitempty"`
	LastDurationMs int            `json:"last_duration_ms"`
}

func (h *Hub) scheduledJobs() []ScheduledJobDefinition {
	return []ScheduledJobDefinition{
		{
			Key:             autoRetentionCronJobID,
			Label:           "Automatic Retention",
			Description:     "Deletes old probe and notification history based on retention settings.",
			DefaultSchedule: autoRetentionCronExpr,
			Run: func() (map[string]any, error) {
				result := h.runAutomaticRetentionPurge()
				payload := map[string]any{
					"monitor_events_deleted":    result.MonitorEventsDeleted,
					"notification_logs_deleted": result.NotificationLogsDeleted,
				}
				if result.Status != "success" {
					if result.Error == "" {
						return payload, fmt.Errorf("job failed")
					}
					return payload, fmt.Errorf("%s", result.Error)
				}
				return payload, nil
			},
		},
		{
			Key:             containerImageAuditCronJobID,
			Label:           "Container Image Audit",
			Description:     "Checks container image tags for newer compatible versions.",
			DefaultSchedule: containerImageAuditCronExpr,
			Run: func() (map[string]any, error) {
				return h.runContainerImageAudit()
			},
		},
		{
			Key:             hostMetricsRetentionCronJobID,
			Label:           "Host Metrics Retention",
			Description:     "Deletes old host metric samples kept only for short-term charts.",
			DefaultSchedule: hostMetricsRetentionCronExpr,
			Run: func() (map[string]any, error) {
				deleted, err := h.purgeHostMetricSamplesOlderThan(hostMetricsRetentionDays)
				payload := map[string]any{"host_metric_samples_deleted": deleted}
				if err != nil {
					return payload, err
				}
				return payload, nil
			},
		},
	}
}

func (h *Hub) scheduledJobByKey(key string) (ScheduledJobDefinition, bool) {
	for _, job := range h.scheduledJobs() {
		if job.Key == key {
			return job, true
		}
	}
	return ScheduledJobDefinition{}, false
}

func (h *Hub) getOrCreateScheduledJobRecord(job ScheduledJobDefinition) (*core.Record, error) {
	rec, err := h.FindFirstRecordByFilter(scheduledJobsCollection, "key = {:key}", dbx.Params{"key": job.Key})
	if err == nil {
		if rec.GetString("schedule") == "" {
			rec.Set("schedule", job.DefaultSchedule)
			if saveErr := h.Save(rec); saveErr != nil {
				return nil, saveErr
			}
		}
		return rec, nil
	}

	col, colErr := h.FindCachedCollectionByNameOrId(scheduledJobsCollection)
	if colErr != nil {
		return nil, colErr
	}
	rec = core.NewRecord(col)
	rec.Set("key", job.Key)
	rec.Set("schedule", job.DefaultSchedule)
	rec.Set("last_status", "idle")
	rec.Set("last_error", "")
	rec.Set("last_result", map[string]any{})
	rec.Set("last_duration_ms", 0)
	if saveErr := h.Save(rec); saveErr != nil {
		return nil, saveErr
	}
	return rec, nil
}

func (h *Hub) effectiveJobSchedule(job ScheduledJobDefinition) string {
	rec, err := h.FindFirstRecordByFilter(scheduledJobsCollection, "key = {:key}", dbx.Params{"key": job.Key})
	if err != nil {
		return job.DefaultSchedule
	}
	if schedule := rec.GetString("schedule"); schedule != "" {
		return schedule
	}
	return job.DefaultSchedule
}

func (h *Hub) syncScheduledJobRecords() error {
	for _, job := range h.scheduledJobs() {
		if _, err := h.getOrCreateScheduledJobRecord(job); err != nil {
			return err
		}
	}
	return nil
}

func scheduledJobRecordToResponse(job ScheduledJobDefinition, rec *core.Record) ScheduledJobRecord {
	result, _ := rec.Get("last_result").(map[string]any)
	if result == nil {
		result = map[string]any{}
	}
	schedule := rec.GetString("schedule")
	if schedule == "" {
		schedule = job.DefaultSchedule
	}
	return ScheduledJobRecord{
		Key:            job.Key,
		Label:          job.Label,
		Description:    job.Description,
		Schedule:       schedule,
		LastRunAt:      rec.GetString("last_run_at"),
		LastSuccessAt:  rec.GetString("last_success_at"),
		LastStatus:     rec.GetString("last_status"),
		LastError:      rec.GetString("last_error"),
		LastResult:     result,
		LastDurationMs: rec.GetInt("last_duration_ms"),
	}
}

func (h *Hub) listScheduledJobs() ([]ScheduledJobRecord, error) {
	jobs := h.scheduledJobs()
	result := make([]ScheduledJobRecord, 0, len(jobs))
	for _, job := range jobs {
		rec, err := h.getOrCreateScheduledJobRecord(job)
		if err != nil {
			return nil, err
		}
		result = append(result, scheduledJobRecordToResponse(job, rec))
	}
	return result, nil
}

func (h *Hub) persistScheduledJobRun(job ScheduledJobDefinition, startedAt time.Time, duration time.Duration, payload map[string]any, runErr error) error {
	rec, err := h.getOrCreateScheduledJobRecord(job)
	if err != nil {
		return err
	}
	rec.Set("last_run_at", startedAt.UTC().Format(time.RFC3339))
	rec.Set("last_duration_ms", duration.Milliseconds())
	rec.Set("last_result", payload)
	if runErr != nil {
		rec.Set("last_status", "failed")
		rec.Set("last_error", runErr.Error())
	} else {
		rec.Set("last_status", "success")
		rec.Set("last_error", "")
		rec.Set("last_success_at", startedAt.UTC().Format(time.RFC3339))
	}
	return h.Save(rec)
}

func (h *Hub) runScheduledJob(job ScheduledJobDefinition) (ScheduledJobRecord, error) {
	startedAt := time.Now().UTC()
	payload, runErr := job.Run()
	duration := time.Since(startedAt)
	if payload == nil {
		payload = map[string]any{}
	}
	if err := h.persistScheduledJobRun(job, startedAt, duration, payload, runErr); err != nil {
		return ScheduledJobRecord{}, err
	}
	rec, err := h.getOrCreateScheduledJobRecord(job)
	if err != nil {
		return ScheduledJobRecord{}, err
	}
	return scheduledJobRecordToResponse(job, rec), runErr
}

func (h *Hub) registerScheduledJobs() error {
	if err := h.syncScheduledJobRecords(); err != nil {
		return err
	}
	for _, job := range h.scheduledJobs() {
		job := job
		schedule := h.effectiveJobSchedule(job)
		if err := h.Cron().Add(job.Key, schedule, func() {
			_, _ = h.runScheduledJob(job)
		}); err != nil {
			return err
		}
	}
	return nil
}

func (h *Hub) updateJobSchedule(job ScheduledJobDefinition, schedule string) error {
	if _, err := cron.NewSchedule(schedule); err != nil {
		return fmt.Errorf("invalid schedule %q: %w", schedule, err)
	}
	rec, err := h.getOrCreateScheduledJobRecord(job)
	if err != nil {
		return err
	}
	rec.Set("schedule", schedule)
	if err := h.Save(rec); err != nil {
		return err
	}
	return h.Cron().Add(job.Key, schedule, func() {
		_, _ = h.runScheduledJob(job)
	})
}
