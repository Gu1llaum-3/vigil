//go:build testing

package hub

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpdateJobScheduleRejectsInvalidCron(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	job, ok := hub.scheduledJobByKey(containerImageAuditCronJobID)
	require.True(t, ok)

	err = hub.updateJobSchedule(job, "not a cron")
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid schedule")
}

func TestUpdateJobSchedulePersistsAndOverridesDefault(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	job, ok := hub.scheduledJobByKey(containerImageAuditCronJobID)
	require.True(t, ok)

	require.Equal(t, containerImageAuditCronExpr, hub.effectiveJobSchedule(job))

	require.NoError(t, hub.updateJobSchedule(job, "*/30 * * * *"))
	require.Equal(t, "*/30 * * * *", hub.effectiveJobSchedule(job))

	jobs, err := hub.listScheduledJobs()
	require.NoError(t, err)
	var found bool
	for _, j := range jobs {
		if j.Key == containerImageAuditCronJobID {
			require.Equal(t, "*/30 * * * *", j.Schedule)
			found = true
		}
	}
	require.True(t, found)
}
