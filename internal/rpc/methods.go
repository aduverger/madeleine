package rpc

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aduverger/madeleine/internal/madeleine"
)

type methodHandler func(context.Context, *madeleine.Service, json.RawMessage) (any, error)

type captureReference struct {
	CaptureID madeleine.CaptureID `json:"capture_id"`
}

var methodHandlers = map[string]methodHandler{
	"capture.start":        startCapture,
	"capture.get":          getCapture,
	"capture.record_write": recordWrite,
	"capture.list_pending": listPendingCaptures,
	"capture.seal":         sealCapture,
	"capture.abandon":      abandonCapture,
	"episode.publish":      publishEpisode,
	"context.for_paths":    contextForPaths,
	"episode.get":          getEpisode,
}

func startCapture(ctx context.Context, service *madeleine.Service, params json.RawMessage) (any, error) {
	var request madeleine.StartCaptureRequest
	if err := decodeParams(params, &request); err != nil {
		return nil, err
	}
	return service.StartCapture(ctx, request)
}

func getCapture(ctx context.Context, service *madeleine.Service, params json.RawMessage) (any, error) {
	var request captureReference
	if err := decodeParams(params, &request); err != nil {
		return nil, err
	}
	return service.GetCapture(ctx, request.CaptureID)
}

func recordWrite(ctx context.Context, service *madeleine.Service, params json.RawMessage) (any, error) {
	var request madeleine.RecordWriteRequest
	if err := decodeParams(params, &request); err != nil {
		return nil, err
	}
	if err := service.RecordWrite(ctx, request); err != nil {
		return nil, err
	}
	return emptyResult{}, nil
}

func listPendingCaptures(ctx context.Context, service *madeleine.Service, params json.RawMessage) (any, error) {
	var request madeleine.PendingCaptureQuery
	if err := decodeParams(params, &request); err != nil {
		return nil, err
	}
	return service.ListPendingCaptures(ctx, request)
}

func sealCapture(ctx context.Context, service *madeleine.Service, params json.RawMessage) (any, error) {
	var request madeleine.SealCaptureRequest
	if err := decodeParams(params, &request); err != nil {
		return nil, err
	}
	return service.SealCapture(ctx, request)
}

func abandonCapture(ctx context.Context, service *madeleine.Service, params json.RawMessage) (any, error) {
	var request captureReference
	if err := decodeParams(params, &request); err != nil {
		return nil, err
	}
	if err := service.AbandonCapture(ctx, request.CaptureID); err != nil {
		return nil, err
	}
	return emptyResult{}, nil
}

func publishEpisode(ctx context.Context, service *madeleine.Service, params json.RawMessage) (any, error) {
	var request madeleine.PublishEpisodeRequest
	if err := decodeParams(params, &request); err != nil {
		return nil, err
	}
	return service.PublishEpisode(ctx, request)
}

func contextForPaths(ctx context.Context, service *madeleine.Service, params json.RawMessage) (any, error) {
	var request madeleine.ContextRequest
	if err := decodeParams(params, &request); err != nil {
		return nil, err
	}
	return service.ContextForPaths(ctx, request)
}

func getEpisode(ctx context.Context, service *madeleine.Service, params json.RawMessage) (any, error) {
	var request madeleine.EpisodeRequest
	if err := decodeParams(params, &request); err != nil {
		return nil, err
	}
	return service.GetEpisode(ctx, request)
}

func decodeParams(params json.RawMessage, destination any) error {
	if err := json.Unmarshal(params, destination); err != nil {
		return fmt.Errorf("%w: params do not match the method", errInvalidParams)
	}
	return nil
}
