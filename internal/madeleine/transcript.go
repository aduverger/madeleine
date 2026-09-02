package madeleine

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/aduverger/madeleine/internal/store"
)

const (
	TranscriptFormatVersion       = 1
	rawTranscriptPageSize         = 50
	rawTranscriptPageMaxJSONBytes = 8 * 1024 * 1024
	maxMutationErrorCharacters    = 1_000
)

func validateTranscriptInput(input *TranscriptInput) error {
	if input == nil || input.FormatVersion != TranscriptFormatVersion || len(input.Entries) == 0 {
		return fmt.Errorf("%w: Capture transcript is missing or unsupported", ErrInvalidState)
	}
	for _, entry := range input.Entries {
		if err := validateTranscriptEntry(entry); err != nil {
			return err
		}
	}
	return nil
}

func validateTranscriptEntry(entry TranscriptEntry) error {
	switch entry.Kind {
	case TranscriptEntryUser, TranscriptEntryAssistant, TranscriptEntryBranchSummary:
		if strings.TrimSpace(entry.Text) == "" || entry.Operation != "" || entry.Path != "" || entry.Status != "" || entry.Error != "" {
			return fmt.Errorf("%w: invalid %s Transcript entry", ErrInvalidState, entry.Kind)
		}
	case TranscriptEntryMutation:
		validOperation := entry.Operation == "edit" || entry.Operation == "write"
		validStatus := entry.Status == "success" || entry.Status == "failure"
		validError := entry.Status == "failure" || entry.Error == ""
		if !validOperation || strings.TrimSpace(entry.Path) == "" || !validStatus || !validError || entry.Text != "" {
			return fmt.Errorf("%w: invalid mutation Transcript entry", ErrInvalidState)
		}
		if utf8.RuneCountInString(entry.Error) > maxMutationErrorCharacters {
			return fmt.Errorf("%w: mutation error exceeds %d Unicode characters", ErrInvalidState, maxMutationErrorCharacters)
		}
	default:
		return fmt.Errorf("%w: unsupported Transcript entry kind %q", ErrInvalidState, entry.Kind)
	}
	return nil
}

func encodeTranscriptEntries(entries []TranscriptEntry) ([]store.TranscriptEntryRecord, error) {
	records := make([]store.TranscriptEntryRecord, len(entries))
	for position, entry := range entries {
		content := entry
		content.Kind = ""
		encoded, err := json.Marshal(content)
		if err != nil {
			return nil, err
		}
		records[position] = store.TranscriptEntryRecord{
			Position:    position,
			Kind:        string(entry.Kind),
			ContentJSON: string(encoded),
		}
	}
	return records, nil
}

func decodeTranscriptEntries(records []store.TranscriptEntryRecord) ([]TranscriptEntry, error) {
	entries := make([]TranscriptEntry, len(records))
	for index, record := range records {
		if err := json.Unmarshal([]byte(record.ContentJSON), &entries[index]); err != nil {
			return nil, err
		}
		entries[index].Kind = TranscriptEntryKind(record.Kind)
		if err := validateTranscriptEntry(entries[index]); err != nil {
			return nil, err
		}
	}
	return entries, nil
}

func transcriptInputMatches(
	input TranscriptInput,
	record store.TranscriptRecord,
	entries []store.TranscriptEntryRecord,
) (bool, error) {
	if input.FormatVersion != record.FormatVersion {
		return false, nil
	}
	storedEntries, err := decodeTranscriptEntries(entries)
	if err != nil {
		return false, err
	}
	return reflect.DeepEqual(input.Entries, storedEntries), nil
}

func (s *Service) GetTranscript(ctx context.Context, request TranscriptRequest) (TranscriptView, error) {
	if request.RepositoryRoot == "" || request.TranscriptID == "" || request.Offset < 0 {
		return TranscriptView{}, wrapError("get Transcript", string(request.TranscriptID), ErrInvalidState)
	}
	if request.View != TranscriptViewCompact && request.View != TranscriptViewRaw {
		return TranscriptView{}, wrapError("get Transcript", string(request.TranscriptID), ErrInvalidState)
	}
	if request.View == TranscriptViewCompact && request.Offset != 0 {
		return TranscriptView{}, wrapError("get Transcript", string(request.TranscriptID), ErrInvalidState)
	}

	repository, err := s.ResolveRepository(ctx, request.RepositoryRoot)
	if err != nil {
		return TranscriptView{}, err
	}
	record, found, err := s.database.GetTranscript(ctx, string(repository.ID), string(request.TranscriptID))
	if err != nil {
		return TranscriptView{}, wrapError("get Transcript", string(request.TranscriptID), err)
	}
	if !found {
		return TranscriptView{}, wrapError("get Transcript", string(request.TranscriptID), ErrNotFound)
	}

	view := TranscriptView{TranscriptID: request.TranscriptID, View: request.View}
	if request.View == TranscriptViewCompact {
		if record.CompactText == nil {
			return TranscriptView{}, wrapError("get Transcript", string(request.TranscriptID), ErrInvalidState)
		}
		view.Compact = *record.CompactText
		return view, nil
	}

	records, err := s.database.TranscriptEntries(
		ctx, string(request.TranscriptID), request.Offset, rawTranscriptPageSize+1,
	)
	if err != nil {
		return TranscriptView{}, wrapError("get Transcript", string(request.TranscriptID), err)
	}
	if request.Offset > 0 && len(records) == 0 {
		return TranscriptView{}, wrapError("get Transcript", string(request.TranscriptID), ErrInvalidState)
	}
	var nextRecordOffset *int
	if len(records) > rawTranscriptPageSize {
		nextOffset := records[rawTranscriptPageSize].Position
		nextRecordOffset = &nextOffset
		records = records[:rawTranscriptPageSize]
	}
	entries, err := decodeTranscriptEntries(records)
	if err != nil {
		return TranscriptView{}, wrapError("get Transcript", string(request.TranscriptID), err)
	}
	visibleCount, err := rawTranscriptPageEntryCount(entries)
	if err != nil {
		return TranscriptView{}, wrapError("get Transcript", string(request.TranscriptID), err)
	}
	if visibleCount < len(entries) {
		nextOffset := records[visibleCount].Position
		view.NextOffset = &nextOffset
		entries = entries[:visibleCount]
	} else {
		view.NextOffset = nextRecordOffset
	}
	view.Entries = entries
	return view, nil
}

func rawTranscriptPageEntryCount(entries []TranscriptEntry) (int, error) {
	totalBytes := 2 // JSON array brackets.
	for index, entry := range entries {
		encoded, err := json.Marshal(entry)
		if err != nil {
			return 0, err
		}
		separatorBytes := 0
		if index > 0 {
			separatorBytes = 1
		}
		if index > 0 && totalBytes+separatorBytes+len(encoded) > rawTranscriptPageMaxJSONBytes {
			return index, nil
		}
		totalBytes += separatorBytes + len(encoded)
	}
	return len(entries), nil
}
