package postgres

import (
	"errors"
	"testing"

	"github.com/getzep/zep/pkg/models"
	"github.com/getzep/zep/pkg/store"
	"github.com/getzep/zep/pkg/testutils"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestPutSummary(t *testing.T) {
	sessionID := createSession(t)

	messages := []models.Message{
		{
			Role:     "user",
			Content:  "Hello",
			Metadata: map[string]interface{}{"timestamp": "1629462540"},
		},
		{
			Role:     "bot",
			Content:  "Hi there!",
			Metadata: map[string]interface{}{"timestamp": 1629462551},
		},
	}

	messageDAO, err := NewMessageDAO(testDB, sessionID)
	assert.NoError(t, err, "NewMessageDAO should not return an error")

	resultMessages, err := messageDAO.CreateMany(testCtx, messages)
	assert.NoError(t, err, "CreateMany should not return an error")

	summaryDAO, err := NewSummaryDAO(testDB, appState, sessionID)
	assert.NoError(t, err, "NewSummaryDAO should not return an error")

	tests := []struct {
		name             string
		sessionID        string
		summary          models.Summary
		SummaryPointUUID uuid.UUID
		wantErr          bool
		errMessage       string
	}{
		{
			name:      "Valid summary",
			sessionID: sessionID,
			summary: models.Summary{
				Content: "Test content",
				Metadata: map[string]interface{}{
					"key": "value",
				},
				SummaryPointUUID: resultMessages[0].UUID,
			},

			wantErr: false,
		},
		{
			name:      "Empty session ID",
			sessionID: "",
			summary: models.Summary{
				Content: "Test content",
				Metadata: map[string]interface{}{
					"key": "value",
				},
				SummaryPointUUID: resultMessages[1].UUID,
			},

			wantErr:    true,
			errMessage: "sessionID cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resultSummary, err := summaryDAO.Create(
				testCtx,
				&tt.summary,
			)

			if tt.wantErr {
				assert.Error(t, err)
				var storageErr *store.StorageError
				if ok := errors.As(err, &storageErr); ok {
					assert.Equal(t, tt.errMessage, storageErr.Message)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, resultSummary)
				assert.NotEmpty(t, resultSummary.UUID)
				assert.False(t, resultSummary.CreatedAt.IsZero())
				assert.Equal(t, tt.summary.Content, resultSummary.Content)
				assert.Equal(t, tt.summary.Metadata, resultSummary.Metadata)
			}
		})
	}
}

func TestGetSummary(t *testing.T) {
	sessionID, err := testutils.GenerateRandomSessionID(16)
	assert.NoError(t, err, "GenerateRandomSessionID should not return an error")
	metadata := map[string]interface{}{
		"key": "value",
	}

	session := &models.CreateSessionRequest{
		SessionID: sessionID,
		Metadata:  metadata,
	}

	sessionManager := NewSessionDAO(testDB)
	_, err = sessionManager.Create(testCtx, session)
	assert.NoError(t, err, "CreateMessages should not return an error")

	summary := models.Summary{
		Content: "Test content",
		Metadata: map[string]interface{}{
			"key": "value",
		},
	}
	summaryTwo := models.Summary{
		Content: "Test content 2",
		Metadata: map[string]interface{}{
			"key": "value",
		},
	}

	messages := []models.Message{
		{
			Role:     "user",
			Content:  "Hello",
			Metadata: map[string]interface{}{"timestamp": "1629462540"},
		},
		{
			Role:     "bot",
			Content:  "Hello!",
			Metadata: map[string]interface{}{"timestamp": "1629462540"},
		},
	}

	messageDAO, err := NewMessageDAO(testDB, sessionID)
	assert.NoError(t, err, "NewMessageDAO should not return an error")

	resultMessages, err := messageDAO.CreateMany(testCtx, messages)
	assert.NoError(t, err, "CreateMany should not return an error")

	summaryDAO, err := NewSummaryDAO(testDB, appState, sessionID)
	assert.NoError(t, err, "NewSummaryDAO should not return an error")

	summary.SummaryPointUUID = resultMessages[0].UUID
	_, err = summaryDAO.Create(testCtx, &summary)
	assert.NoError(t, err, "Create should not return an error")

	summaryTwo.SummaryPointUUID = resultMessages[1].UUID
	putSummaryResultTwo, err := summaryDAO.Create(testCtx, &summaryTwo)
	assert.NoError(t, err, "putSummary2 should not return an error")

	tests := []struct {
		name          string
		sessionID     string
		expectedFound bool
	}{
		{
			name:          "Existing summary",
			sessionID:     sessionID,
			expectedFound: true,
		},
		{
			name:          "Non-existent session",
			sessionID:     "nonexistent",
			expectedFound: false,
		},
		// Add more test cases as needed
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := summaryDAO.Get(testCtx)
			assert.NoError(t, err)

			if tt.expectedFound {
				assert.NotNil(t, result)
				// Ensure it is the last summary added
				assert.Equal(t, putSummaryResultTwo.UUID, result.UUID)
				assert.False(t, result.CreatedAt.IsZero())
				assert.Equal(t, putSummaryResultTwo.Content, result.Content)
				assert.Equal(t, putSummaryResultTwo.Metadata, result.Metadata)
			} else {
				assert.Nil(t, result)
			}
		})
	}
}

func TestPostgresMemoryStore_GetSummaryByUUID(t *testing.T) {
	sessionID := createSession(t)

	messages := []models.Message{
		{
			Role:     "user",
			Content:  "Hello",
			Metadata: map[string]interface{}{"timestamp": "1629462540"},
		},
		{
			Role:     "bot",
			Content:  "Hi there!",
			Metadata: map[string]interface{}{"timestamp": 1629462551},
		},
	}

	messageDAO, err := NewMessageDAO(testDB, sessionID)
	assert.NoError(t, err, "NewMessageDAO should not return an error")

	resultMessages, err := messageDAO.CreateMany(testCtx, messages)
	assert.NoError(t, err, "CreateMany should not return an error")

	summary := models.Summary{
		Content: "Test content",
		Metadata: map[string]interface{}{
			"key": "value",
		},
		SummaryPointUUID: resultMessages[0].UUID,
	}

	summaryDAO, err := NewSummaryDAO(testDB, appState, sessionID)
	assert.NoError(t, err, "NewSummaryDAO should not return an error")

	// Call putSummary function
	resultSummary, err := summaryDAO.Create(testCtx, &summary)
	assert.NoError(t, err, "Create should not return an error")

	tests := []struct {
		name          string
		sessionID     string
		uuid          uuid.UUID
		expectedFound bool
	}{
		{
			name:          "Existing summary",
			sessionID:     sessionID,
			uuid:          resultSummary.UUID,
			expectedFound: true,
		},
		{
			name:          "Non-existent summary",
			sessionID:     sessionID,
			uuid:          uuid.New(),
			expectedFound: false,
		},
		{
			name:          "Non-existent session",
			sessionID:     "nonexistent",
			uuid:          uuid.New(),
			expectedFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := summaryDAO.GetByUUID(
				testCtx,
				tt.uuid,
			)

			if tt.expectedFound {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				assert.Equal(t, resultSummary.UUID, result.UUID)
				assert.False(t, result.CreatedAt.IsZero())
				assert.Equal(t, resultSummary.Content, result.Content)
				assert.Equal(t, resultSummary.Metadata, result.Metadata)
			} else {
				assert.Nil(t, result)
				assert.ErrorIs(t, err, models.ErrNotFound)
			}
		})
	}
}

func TestPostgresMemoryStore_PutSummaryEmbedding(t *testing.T) {
	sessionID := createSession(t)

	messages := []models.Message{
		{
			Role:     "user",
			Content:  "Hello",
			Metadata: map[string]interface{}{"timestamp": "1629462540"},
		},
		{
			Role:     "bot",
			Content:  "Hi there!",
			Metadata: map[string]interface{}{"timestamp": 1629462551},
		},
	}

	messageDAO, err := NewMessageDAO(testDB, sessionID)
	assert.NoError(t, err, "NewMessageDAO should not return an error")

	resultMessages, err := messageDAO.CreateMany(testCtx, messages)
	assert.NoError(t, err, "CreateMany should not return an error")

	summary := models.Summary{
		Content: "Test content",
		Metadata: map[string]interface{}{
			"key": "value",
		},
		SummaryPointUUID: resultMessages[0].UUID,
	}

	summaryDAO, err := NewSummaryDAO(testDB, appState, sessionID)
	assert.NoError(t, err, "NewSummaryDAO should not return an error")

	// Call putSummary function
	resultSummary, err := summaryDAO.Create(testCtx, &summary)
	assert.NoError(t, err, "Create should not return an error")

	v := make([]float32, appState.Config.Extractors.Messages.Summarizer.Embeddings.Dimensions)

	embedding := models.TextData{
		Embedding: v,
		TextUUID:  resultSummary.UUID,
		Text:      resultSummary.Content,
	}

	err = summaryDAO.PutEmbedding(
		testCtx,
		&embedding,
	)
	assert.NoError(t, err, "putSummaryEmbedding should not return an error")
}

func TestGetSummaryList(t *testing.T) {
	// CreateMessages a test session
	sessionID, err := testutils.GenerateRandomSessionID(16)
	assert.NoError(t, err, "GenerateRandomSessionID should not return an error")

	messageDAO, err := NewMessageDAO(testDB, sessionID)
	assert.NoError(t, err, "NewMessageDAO should not return an error")

	msgs, err := messageDAO.CreateMany(testCtx, testutils.TestMessages)
	assert.NoError(t, err, "CreateMany should not return an error")

	summaryDAO, err := NewSummaryDAO(testDB, appState, sessionID)
	assert.NoError(t, err, "NewSummaryDAO should not return an error")

	// Add test summaries
	for i := 0; i < 9; i++ {
		summary := models.Summary{
			Content: "Test content",
			Metadata: map[string]interface{}{
				"key": "value",
			},
			SummaryPointUUID: msgs[i].UUID,
		}
		_, err = summaryDAO.Create(testCtx, &summary)
		assert.NoError(t, err, "putSummary should not return an error")
	}

	// Define test cases
	tests := []struct {
		name          string
		sessionID     string
		pageNumber    int
		pageSize      int
		expectedCount int
	}{
		{
			name:          "Existing session",
			sessionID:     sessionID,
			pageNumber:    1,
			pageSize:      5,
			expectedCount: 5,
		},
		{
			name:          "Existing session page 2",
			sessionID:     sessionID,
			pageNumber:    2,
			pageSize:      5,
			expectedCount: 4,
		},
		{
			name:          "Non-existent session",
			sessionID:     "nonexistent",
			pageNumber:    1,
			pageSize:      10,
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summaries, err := summaryDAO.GetList(
				testCtx,
				tt.pageNumber,
				tt.pageSize,
			)
			assert.NoError(t, err)

			// Check the number of summaries returned
			assert.Equal(t, tt.expectedCount, len(summaries.Summaries))
		})
	}
}

func TestUpdateSummaryMetadata(t *testing.T) {
	// Step 1: CreateMessages a session
	sessionID := createSession(t)

	// Step 2: Create test messages
	messages := []models.Message{
		{
			Role:    "user",
			Content: "Hello",
		},
		{
			Role:    "bot",
			Content: "Hi there!",
		},
	}

	messageDAO, err := NewMessageDAO(testDB, sessionID)
	assert.NoError(t, err, "NewMessageDAO should not return an error")

	returnedMessages, err := messageDAO.CreateMany(testCtx, messages)
	assert.NoError(t, err, "CreateMany should not return an error")

	// Step 3: Use putSummary to add a new test summary
	summary := models.Summary{
		SummaryPointUUID: returnedMessages[0].UUID,
		Metadata: map[string]interface{}{
			"key1": "value1",
			"key2": "value2",
		},
	}

	summaryDAO, err := NewSummaryDAO(testDB, appState, sessionID)
	assert.NoError(t, err, "NewSummaryDAO should not return an error")

	returnedSummary, err := summaryDAO.Create(testCtx, &summary)
	assert.NoError(t, err, "putSummary should not return an error")

	// Step 4: UpdateSummaryMetadata to update the metadata
	newMetadata := map[string]interface{}{
		"key1": "new value1",
		"key2": "new value2",
	}
	returnedSummary.Metadata = newMetadata
	_, err = summaryDAO.UpdateMetadata(testCtx, returnedSummary)
	assert.NoError(t, err, "updateSummaryMetadata should not return an error")

	// Step 5: GetSummary to test that the metadata was correctly updated
	resultSummary, err := summaryDAO.Get(testCtx)
	assert.NoError(t, err, "GetSummary should not return an error")
	assert.Equal(t, newMetadata, resultSummary.Metadata)
}
