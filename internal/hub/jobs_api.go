package hub

import (
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

func (h *Hub) getScheduledJobs(e *core.RequestEvent) error {
	jobs, err := h.listScheduledJobs()
	if err != nil {
		return err
	}
	return e.JSON(http.StatusOK, jobs)
}

func (h *Hub) runScheduledJobNow(e *core.RequestEvent) error {
	key := e.Request.PathValue("key")
	job, ok := h.scheduledJobByKey(key)
	if !ok {
		return e.NotFoundError("Scheduled job not found", nil)
	}
	result, err := h.runScheduledJob(job)
	if err != nil {
		return e.JSON(http.StatusOK, result)
	}
	return e.JSON(http.StatusOK, result)
}

func (h *Hub) updateScheduledJob(e *core.RequestEvent) error {
	key := e.Request.PathValue("key")
	job, ok := h.scheduledJobByKey(key)
	if !ok {
		return e.NotFoundError("Scheduled job not found", nil)
	}

	var body struct {
		Schedule string `json:"schedule"`
	}
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("Invalid request body", err)
	}
	body.Schedule = strings.TrimSpace(body.Schedule)
	if body.Schedule == "" {
		return e.BadRequestError("Schedule is required", nil)
	}

	if err := h.updateJobSchedule(job, body.Schedule); err != nil {
		return e.BadRequestError(err.Error(), err)
	}

	rec, err := h.getOrCreateScheduledJobRecord(job)
	if err != nil {
		return err
	}
	return e.JSON(http.StatusOK, scheduledJobRecordToResponse(job, rec))
}
