package sessionsync

const (
	ErrorContentProjectionNotReady = "CONTENT_PROJECTION_NOT_READY"
	ErrorRevisionHighWaterChanged  = "REVISION_HIGH_WATER_CHANGED"
)

type ProjectionActivationStatus string

const (
	ProjectionReady   ProjectionActivationStatus = "ready"
	ProjectionCatchUp ProjectionActivationStatus = "catch_up"
	ProjectionInvalid ProjectionActivationStatus = "invalid"
)

type ProjectionActivationDecision struct {
	Status               ProjectionActivationStatus
	SourceHighWater      int64
	ContentIndexedCursor int64
	ErrorCode            string
}

func DecideProjectionActivation(sourceHighWater, contentIndexedCursor int64) ProjectionActivationDecision {
	decision := ProjectionActivationDecision{
		SourceHighWater:      sourceHighWater,
		ContentIndexedCursor: contentIndexedCursor,
	}
	if sourceHighWater < 0 || contentIndexedCursor < 0 || contentIndexedCursor > sourceHighWater {
		decision.Status = ProjectionInvalid
		decision.ErrorCode = ErrorContentProjectionNotReady
		return decision
	}
	if contentIndexedCursor < sourceHighWater {
		decision.Status = ProjectionCatchUp
		decision.ErrorCode = ErrorRevisionHighWaterChanged
		return decision
	}
	decision.Status = ProjectionReady
	return decision
}

func CanReadProjectedRange(contentIndexedCursor, requestedEndCursor int64) (bool, string) {
	if contentIndexedCursor < 0 || requestedEndCursor < 0 || requestedEndCursor > contentIndexedCursor {
		return false, ErrorContentProjectionNotReady
	}
	return true, ""
}
