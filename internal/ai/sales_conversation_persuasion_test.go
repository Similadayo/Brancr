package ai

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/testutil"
)

func TestRecordAIPersuasion_IncrementsCounter(t *testing.T) {
	db := testutil.TestDB(t)
	defer testutil.CleanupDB(t, db)

	db.AutoMigrate(&storage.SalesConversation{})

	sc := &storage.SalesConversation{ConversationID: uuid.New(), TenantID: uuid.New(), CustomerID: uuid.New()}
	db.Create(sc)

	mgr := &SalesConversationManager{DB: db}
	if err := mgr.RecordAIPersuasion(context.Background(), sc.ID); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	var updated storage.SalesConversation
	db.First(&updated, sc.ID)
	if updated.AIPersuasionCount != 1 {
		t.Fatalf("expected AIPersuasionCount=1, got %d", updated.AIPersuasionCount)
	}
}
