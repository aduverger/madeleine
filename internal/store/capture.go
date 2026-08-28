package store

import "fmt"

type captureAction uint8

const (
	captureActionRecordWrite captureAction = iota
	captureActionSeal
	captureActionAbandon
)

func transitionCapture(status CaptureStatus, action captureAction, hasPaths bool) (CaptureStatus, error) {
	switch action {
	case captureActionRecordWrite:
		if status == CaptureStatusOpen {
			return status, nil
		}
	case captureActionSeal:
		switch status {
		case CaptureStatusOpen:
			if hasPaths {
				return CaptureStatusPendingSummary, nil
			}
			return CaptureStatusAbandoned, nil
		case CaptureStatusPendingSummary, CaptureStatusFinalized, CaptureStatusAbandoned:
			return status, nil
		}
	case captureActionAbandon:
		switch status {
		case CaptureStatusOpen, CaptureStatusPendingSummary:
			return CaptureStatusAbandoned, nil
		case CaptureStatusAbandoned:
			return status, nil
		}
	}
	return "", fmt.Errorf("%w: cannot apply action %d to Capture status %q", ErrInvalidState, action, status)
}
