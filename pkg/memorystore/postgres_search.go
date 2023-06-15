package memorystore

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/getzep/zep/pkg/llms"
	"github.com/getzep/zep/pkg/models"
	"github.com/pgvector/pgvector-go"
	"github.com/uptrace/bun"
)

const defaultSearchLimit = 10

type JSONQuery struct {
	JSONPath string       `json:"jsonpath"`
	And      []*JSONQuery `json:"and,omitempty"`
	Or       []*JSONQuery `json:"or,omitempty"`
}

func searchMessages(
	ctx context.Context,
	appState *models.AppState,
	db *bun.DB,
	sessionID string,
	query *models.MemorySearchPayload,
	limit int,
) ([]models.MemorySearchResult, error) {
	logrus.Debugf("searchMessages called for session %s", sessionID)

	if query == nil || appState == nil {
		return nil, NewStorageError("nil query or appState received", nil)
	}

	if query.Text == "" && len(query.Metadata) == 0 {
		return nil, NewStorageError("empty query", errors.New("empty query"))
	}

	dbQuery := buildMessagesSelectQuery(ctx, appState, db, query)
	if len(query.Metadata) > 0 {
		var err error
		dbQuery, err = applyMessagesMetadataFilter(dbQuery, query.Metadata)
		if err != nil {
			return nil, NewStorageError("error applying metadata filter", err)
		}
	}

	dbQuery = dbQuery.Where("m.session_id = ?", sessionID)

	// Ensure we don't return deleted records.
	dbQuery = dbQuery.Where("m.deleted_at IS NULL")

	// Add sort and limit.
	addMessagesSortQuery(query.Text, dbQuery)

	if limit == 0 {
		limit = defaultSearchLimit
	}
	dbQuery = dbQuery.Limit(limit)

	results, err := executeMessagesSearchScan(ctx, dbQuery)
	if err != nil {
		return nil, NewStorageError("memory searchMessages failed", err)
	}

	filteredResults := filterValidMessageSearchResults(results, query.Metadata)
	logrus.Debugf("searchMessages completed for session %s", sessionID)

	return filteredResults, nil
}

func buildMessagesSelectQuery(
	ctx context.Context,
	appState *models.AppState,
	db *bun.DB,
	query *models.MemorySearchPayload,
) *bun.SelectQuery {
	dbQuery := db.NewSelect().TableExpr("message_embedding AS me").
		Join("JOIN message AS m").
		JoinOn("me.message_uuid = m.uuid").
		ColumnExpr("m.uuid AS message__uuid").
		ColumnExpr("m.created_at AS message__created_at").
		ColumnExpr("m.role AS message__role").
		ColumnExpr("m.content AS message__content").
		ColumnExpr("m.metadata AS message__metadata").
		ColumnExpr("m.token_count AS message__token_count")

	if query.Text != "" {
		dbQuery, _ = addMessagesVectorColumn(ctx, appState, dbQuery, query.Text)
	}

	return dbQuery
}

func applyMessagesMetadataFilter(
	dbQuery *bun.SelectQuery,
	metadata map[string]interface{},
) (*bun.SelectQuery, error) {
	qb := dbQuery.QueryBuilder()

	if where, ok := metadata["where"]; ok {
		j, err := json.Marshal(where)
		if err != nil {
			return nil, NewStorageError("error marshalling metadata", err)
		}

		var jq JSONQuery
		err = json.Unmarshal(j, &jq)
		if err != nil {
			return nil, NewStorageError("error unmarshalling metadata", err)
		}
		qb = parseJSONQuery(qb, &jq, false)
	}

	addMessageDateFilters(&qb, metadata)

	dbQuery = qb.Unwrap().(*bun.SelectQuery)

	return dbQuery, nil
}

func addMessagesSortQuery(searchText string, dbQuery *bun.SelectQuery) {
	if searchText != "" {
		dbQuery.Order("dist ASC")
	} else {
		dbQuery.Order("m.created_at DESC")
	}
}

func executeMessagesSearchScan(
	ctx context.Context,
	dbQuery *bun.SelectQuery,
) ([]models.MemorySearchResult, error) {
	var results []models.MemorySearchResult
	err := dbQuery.Scan(ctx, &results)
	return results, err
}

func filterValidMessageSearchResults(
	results []models.MemorySearchResult,
	metadata map[string]interface{},
) []models.MemorySearchResult {
	var filteredResults []models.MemorySearchResult
	for _, result := range results {
		if !math.IsNaN(result.Dist) || len(metadata) > 0 {
			filteredResults = append(filteredResults, result)
		}
	}
	return filteredResults
}

// addMessageDateFilters adds date filters to the query
func addMessageDateFilters(qb *bun.QueryBuilder, m map[string]interface{}) {
	if startDate, ok := m["start_date"]; ok {
		*qb = (*qb).Where("m.created_at >= ?", startDate)
	}
	if endDate, ok := m["end_date"]; ok {
		*qb = (*qb).Where("m.created_at <= ?", endDate)
	}
}

// addMessagesVectorColumn adds a column to the query that calculates the distance between the query text and the message embedding
func addMessagesVectorColumn(
	ctx context.Context,
	appState *models.AppState,
	q *bun.SelectQuery,
	queryText string,
) (*bun.SelectQuery, error) {
	model, err := llms.GetMessageEmbeddingModel(appState)
	if err != nil {
		return nil, NewStorageError("failed to get message embedding model", err)
	}

	e, err := llms.EmbedTexts(ctx, appState, model, []string{queryText})
	if err != nil {
		return nil, NewStorageError("failed to embed query", err)
	}

	vector := pgvector.NewVector(e[0])
	return q.ColumnExpr("embedding <#> ? AS dist", vector), nil
}

// parseJSONQuery recursively parses a JSONQuery and returns a bun.QueryBuilder.
// TODO: fix the addition of extraneous parentheses in the query
func parseJSONQuery(qb bun.QueryBuilder, jq *JSONQuery, isOr bool) bun.QueryBuilder {
	if jq.JSONPath != "" {
		path := strings.ReplaceAll(jq.JSONPath, "'", "\"")
		if isOr {
			qb = qb.WhereOr(
				"jsonb_path_exists(m.metadata, ?)",
				path,
			)
		} else {
			qb = qb.Where(
				"jsonb_path_exists(m.metadata, ?)",
				path,
			)
		}
	}

	if len(jq.And) > 0 {
		qb = qb.WhereGroup(" AND ", func(qq bun.QueryBuilder) bun.QueryBuilder {
			for _, subQuery := range jq.And {
				qq = parseJSONQuery(qq, subQuery, false)
			}
			return qq
		})
	}

	if len(jq.Or) > 0 {
		qb = qb.WhereGroup(" AND ", func(qq bun.QueryBuilder) bun.QueryBuilder {
			for _, subQuery := range jq.Or {
				qq = parseJSONQuery(qq, subQuery, true)
			}
			return qq
		})
	}

	return qb
}
