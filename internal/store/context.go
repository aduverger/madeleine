package store

import (
	"context"
	"fmt"
	"strings"
)

func (db *DB) EpisodeSummariesForPaths(
	ctx context.Context,
	repositoryID string,
	paths []string,
	limit int,
) ([]EpisodeSummaryRecord, error) {
	placeholders := make([]string, len(paths))
	arguments := make([]any, 0, len(paths)+3)
	arguments = append(arguments, repositoryID)
	for index, path := range paths {
		placeholders[index] = "?"
		arguments = append(arguments, path)
	}
	arguments = append(arguments, repositoryID, limit)

	query := `
		WITH ranked_episodes AS (
			SELECT files.path, episode.id, episode.ended_at, episode.harness, episode.l1,
				ROW_NUMBER() OVER (
					PARTITION BY files.path
					ORDER BY episode.ended_at DESC, episode.id DESC
				) AS recency_rank
			FROM episode_files files
			JOIN episodes episode ON episode.id = files.episode_id
			WHERE files.repository_id = ?
				AND files.path IN (` + strings.Join(placeholders, ", ") + `)
				AND episode.repository_id = ?
		)
		SELECT path, id, ended_at, harness, l1
		FROM ranked_episodes
		WHERE recency_rank <= ?
		ORDER BY path, ended_at DESC, id DESC`
	rows, err := db.db.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	summaries := make([]EpisodeSummaryRecord, 0)
	for rows.Next() {
		var summary EpisodeSummaryRecord
		var endedAt string
		if err := rows.Scan(
			&summary.Path, &summary.EpisodeID, &endedAt, &summary.Harness, &summary.L1,
		); err != nil {
			return nil, err
		}
		summary.EndedAt, err = parseTimestamp(endedAt)
		if err != nil {
			return nil, fmt.Errorf("parse Episode end time: %w", err)
		}
		summaries = append(summaries, summary)
	}
	return summaries, rows.Err()
}
