package sessionsync

import "time"

type JobStatus string

const (
	JobPending   JobStatus = "pending"
	JobLeased    JobStatus = "leased"
	JobRetryWait JobStatus = "retry_wait"
	JobCompleted JobStatus = "completed"
	JobDead      JobStatus = "dead"
)

type ProcessingJobState struct {
	Status      JobStatus
	Attempts    int
	MaxAttempts int
	LeaseOwner  string
	LeaseUntil  time.Time
	HeartbeatAt time.Time
	NextRetryAt time.Time
	LastError   string
}

func (j *ProcessingJobState) Lease(now time.Time, owner string, ttl time.Duration) bool {
	if owner == "" || ttl <= 0 || j.MaxAttempts <= 0 || j.Attempts >= j.MaxAttempts {
		return false
	}
	eligible := j.Status == JobPending ||
		(j.Status == JobRetryWait && !j.NextRetryAt.After(now)) ||
		(j.Status == JobLeased && !j.LeaseUntil.After(now))
	if !eligible {
		return false
	}
	j.Status = JobLeased
	j.Attempts++
	j.LeaseOwner = owner
	j.LeaseUntil = now.Add(ttl)
	j.HeartbeatAt = now
	j.NextRetryAt = time.Time{}
	return true
}

func (j *ProcessingJobState) Heartbeat(now time.Time, owner string, ttl time.Duration) bool {
	if j.Status != JobLeased || j.LeaseOwner != owner || ttl <= 0 || !j.LeaseUntil.After(now) {
		return false
	}
	j.HeartbeatAt = now
	j.LeaseUntil = now.Add(ttl)
	return true
}

func (j *ProcessingJobState) Complete(owner string, completedAt time.Time) bool {
	if j.Status != JobLeased || j.LeaseOwner != owner || !j.LeaseUntil.After(completedAt) {
		return false
	}
	j.Status = JobCompleted
	j.LeaseOwner = ""
	j.LeaseUntil = time.Time{}
	j.NextRetryAt = time.Time{}
	j.LastError = ""
	return true
}

func (j *ProcessingJobState) Fail(owner string, now time.Time, retryAfter time.Duration, failure string) bool {
	if j.Status != JobLeased || j.LeaseOwner != owner || !j.LeaseUntil.After(now) || retryAfter < 0 {
		return false
	}
	j.LeaseOwner = ""
	j.LeaseUntil = time.Time{}
	j.LastError = failure
	if j.Attempts >= j.MaxAttempts {
		j.Status = JobDead
		j.NextRetryAt = time.Time{}
		return true
	}
	j.Status = JobRetryWait
	j.NextRetryAt = now.Add(retryAfter)
	return true
}
