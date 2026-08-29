package rpc

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/aduverger/madeleine/internal/madeleine"
)

type Config struct {
	Home string
}

func Run(ctx context.Context, method string, input io.Reader, output, diagnostics io.Writer, config Config) Outcome {
	params, requestError := decodeRequest(input)
	if requestError != nil {
		return writeBoundaryResponse(output, diagnostics, requestError)
	}

	handler, exists := methodHandlers[method]
	if !exists {
		return writeBoundaryResponse(output, diagnostics, unknownMethod(method))
	}

	service, err := madeleine.Open(ctx, madeleine.Options{Home: config.Home})
	if err != nil {
		return writeBoundaryResponse(output, diagnostics, mapOperationError(err))
	}

	result, err := handler(ctx, service, params)
	var response responseEnvelope
	outcome := OutcomeSuccess
	if err == nil {
		response = successResponse(result)
	} else {
		var mapped *boundaryError
		if errors.Is(err, errInvalidParams) {
			mapped = invalidRequest("params do not match the method")
		} else {
			mapped = mapOperationError(err)
		}
		response = errorResponse(mapped)
		outcome = mapped.outcome
	}

	if err := encodeResponse(output, response); err != nil {
		fmt.Fprintf(diagnostics, "encode RPC response: %v\n", err)
		outcome = OutcomeOperationFailure
	}
	if err := service.Close(); err != nil {
		fmt.Fprintf(diagnostics, "close Madeleine service: %v\n", err)
		outcome = OutcomeOperationFailure
	}
	return outcome
}

func writeBoundaryResponse(output, diagnostics io.Writer, err *boundaryError) Outcome {
	if encodeErr := encodeResponse(output, errorResponse(err)); encodeErr != nil {
		fmt.Fprintf(diagnostics, "encode RPC response: %v\n", encodeErr)
		return OutcomeOperationFailure
	}
	return err.outcome
}
