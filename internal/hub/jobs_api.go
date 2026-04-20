package hub

import (
	"net/http"

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
