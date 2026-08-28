package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type repositoryAlias struct {
	kind  string
	value string
}

const (
	repositoryAliasCommonGitDir = "common_git_dir"
	repositoryAliasWorktreeRoot = "worktree_root"
	repositoryAliasOrigin       = "origin"
)

func (s *Store) ResolveRepository(ctx context.Context, path string) (Repository, error) {
	facts, err := ResolveRepository(ctx, path)
	if err != nil {
		return Repository{}, err
	}

	if err := withImmediateTransaction(ctx, s.db, func(transaction *sql.Tx) error {
		return persistRepository(ctx, transaction, &facts)
	}); err != nil {
		return Repository{}, wrapError("persist repository", path, err)
	}
	return facts, nil
}

func persistRepository(ctx context.Context, transaction *sql.Tx, repository *Repository) error {
	aliases := aliasesForRepository(*repository)
	existingAliases := make(map[repositoryAlias]bool, len(aliases))
	var matchedRepositoryID RepositoryID

	for _, alias := range aliases {
		var repositoryID RepositoryID
		err := transaction.QueryRowContext(ctx,
			"SELECT repository_id FROM repository_aliases WHERE kind = ? AND value = ?",
			alias.kind, alias.value).Scan(&repositoryID)
		switch {
		case err == nil:
			existingAliases[alias] = true
			if matchedRepositoryID != "" && matchedRepositoryID != repositoryID {
				return fmt.Errorf("%w: repository aliases match different repositories", ErrConflict)
			}
			matchedRepositoryID = repositoryID
		case errors.Is(err, sql.ErrNoRows):
		default:
			return err
		}
	}

	createdAt := utcTimestamp()
	if matchedRepositoryID == "" {
		repositoryID, err := newRepositoryID()
		if err != nil {
			return err
		}
		repository.ID = repositoryID
		if _, err := transaction.ExecContext(ctx,
			"INSERT INTO repositories(id, created_at) VALUES (?, ?)", repository.ID, createdAt); err != nil {
			return err
		}
	} else {
		repository.ID = matchedRepositoryID
	}

	for _, alias := range aliases {
		if existingAliases[alias] {
			continue
		}
		if _, err := transaction.ExecContext(ctx, `
			INSERT INTO repository_aliases(repository_id, kind, value, created_at)
			VALUES (?, ?, ?, ?)`, repository.ID, alias.kind, alias.value, createdAt); err != nil {
			return err
		}
	}
	return nil
}

func aliasesForRepository(repository Repository) []repositoryAlias {
	aliases := []repositoryAlias{
		{kind: repositoryAliasCommonGitDir, value: repository.GitCommonDir},
		{kind: repositoryAliasWorktreeRoot, value: repository.WorktreeRoot},
	}
	if repository.Origin != "" {
		aliases = append(aliases, repositoryAlias{kind: repositoryAliasOrigin, value: repository.Origin})
	}
	return aliases
}
