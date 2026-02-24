package repositories

import (
	"testing"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/testutil"
	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) *gorm.DB {
	db := testutil.TestDB(t)
	if err := db.AutoMigrate(&storage.Draft{}); err != nil {
		t.Fatalf("auto migrate failed: %v", err)
	}
	return db
}

func TestDraftRepo_CreateGet(t *testing.T) {
	db := setupTestDB(t)
	repo := NewDraftRepo(db)
	d := &storage.Draft{TenantID: uuid.Nil, Key: "compose.post", Content: `{"text":"hello"}`}
	if err := repo.Create(d); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	got, err := repo.GetByID(d.ID)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if got.Key != d.Key || got.Content != d.Content {
		t.Fatalf("mismatch: got %v expected %v", got, d)
	}
}
