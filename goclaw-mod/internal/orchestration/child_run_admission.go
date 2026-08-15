package orchestration

import "github.com/nextlevelbuilder/goclaw/internal/childrun"

var (
	ErrChildRunBusy                = childrun.ErrChildRunBusy
	ErrChildRunClosed              = childrun.ErrChildRunClosed
	ErrChildRunDrainTimeout        = childrun.ErrChildRunDrainTimeout
	ErrChildRunInvalidContinuation = childrun.ErrChildRunInvalidContinuation
	ErrChildRunContinuationDepth   = childrun.ErrChildRunContinuationDepth
)

type ChildRunState = childrun.ChildRunState

const (
	ChildRunNew          = childrun.ChildRunNew
	ChildRunQueued       = childrun.ChildRunQueued
	ChildRunRunning      = childrun.ChildRunRunning
	ChildRunWaitingChild = childrun.ChildRunWaitingChild
	ChildRunCompleted    = childrun.ChildRunCompleted
	ChildRunFailed       = childrun.ChildRunFailed
	ChildRunCancelled    = childrun.ChildRunCancelled
)

type ChildRunConstraints = childrun.ChildRunConstraints
type ChildRunStats = childrun.ChildRunStats
type ChildRunAdmission = childrun.ChildRunAdmission
type ChildRunTicket = childrun.ChildRunTicket
type ChildRunLease = childrun.ChildRunLease

func NewChildRunAdmission(processLimit, pendingLimit int) *ChildRunAdmission {
	return childrun.NewChildRunAdmission(processLimit, pendingLimit)
}
