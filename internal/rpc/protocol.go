package rpc

import (
	"encoding/json"
	"fmt"
	"io"
)

const ProtocolVersion = 1

type requestEnvelope struct {
	ProtocolVersion *int             `json:"protocol_version"`
	Params          *json.RawMessage `json:"params"`
}

type responseEnvelope struct {
	ProtocolVersion int            `json:"protocol_version"`
	OK              bool           `json:"ok"`
	Result          any            `json:"result,omitempty"`
	Error           *responseError `json:"error,omitempty"`
}

type responseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type emptyResult struct{}

func decodeRequest(input io.Reader) (json.RawMessage, *boundaryError) {
	payload, err := io.ReadAll(input)
	if err != nil {
		return nil, invalidRequest("read request")
	}

	var request *requestEnvelope
	if err := json.Unmarshal(payload, &request); err != nil || request == nil {
		return nil, invalidRequest("request must be one JSON object")
	}
	if request.ProtocolVersion == nil {
		return nil, unsupportedProtocol("protocol_version is required")
	}
	if *request.ProtocolVersion != ProtocolVersion {
		return nil, unsupportedProtocol(fmt.Sprintf("protocol version %d is not supported", *request.ProtocolVersion))
	}
	if request.Params == nil {
		return nil, invalidRequest("params are required")
	}
	return *request.Params, nil
}

func WriteSuccessResponse(output io.Writer, result any) error {
	return encodeResponse(output, successResponse(result))
}

func encodeResponse(output io.Writer, response responseEnvelope) error {
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(response)
}

func successResponse(result any) responseEnvelope {
	return responseEnvelope{ProtocolVersion: ProtocolVersion, OK: true, Result: result}
}

func errorResponse(err *boundaryError) responseEnvelope {
	return responseEnvelope{
		ProtocolVersion: ProtocolVersion,
		OK:              false,
		Error:           &responseError{Code: err.code, Message: err.message},
	}
}
