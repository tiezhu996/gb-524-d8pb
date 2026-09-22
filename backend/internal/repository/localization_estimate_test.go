package repository

import (
	"context"
	"testing"

	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"spectrum-interference-triangulation/backend/internal/constants"
	"spectrum-interference-triangulation/backend/internal/dto"
	"spectrum-interference-triangulation/backend/internal/model"
)

func newEstimateTestDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:estimate-repository-test?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.ReceiverStation{}, &model.InterferenceCase{}, &model.BearingObservation{}, &model.LocalizationEstimate{}, &model.AuditEvent{}); err != nil {
		t.Fatalf("migrate sqlite database: %v", err)
	}
	return db
}

func TestCreateRunRejectsConcurrentDuplicateEvidence(t *testing.T) {
	db := newEstimateTestDatabase(t)
	user := model.User{Email: "analyst@example.test", DisplayName: "Analyst", PasswordHash: "hash", Role: constants.RoleAnalyst, Active: true}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	caseRecord := model.InterferenceCase{CaseCode: "RF-DUP", Title: "Duplicate", FrequencyCenterHz: 100, CaseStatus: constants.CaseAnalyzing, Version: 1, OpenedBy: user.ID}
	if err := db.Create(&caseRecord).Error; err != nil {
		t.Fatalf("create case: %v", err)
	}
	hash := "same-evidence-hash"
	primary := model.LocalizationEstimate{
		CaseID: caseRecord.ID, BatchIndex: 1, AlgorithmVersion: "test", ConditionLimit: 1000,
		UsedObservationIDsJSON: datatypes.JSON(`[1,2,3]`), OutlierIDsJSON: datatypes.JSON(`[]`),
		ResidualsJSON: datatypes.JSON(`[]`), InputSnapshotJSON: datatypes.JSON(`[]`),
		BatchSnapshotJSON: datatypes.JSON(`{}`), EvidenceHash: &hash, EstimateStatus: constants.EstimateComplete,
		CreatedBy: user.ID,
	}
	repo := NewEstimateRepository(db)
	actor := Actor{UserID: user.ID, Email: user.Email, Role: user.Role, RequestID: "request-1"}
	if err := repo.CreateRun(context.Background(), caseRecord.ID, caseRecord.Version, &primary, nil, false, 1000, dto.LocalizationBatch{}, actor); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if err := db.Model(&model.InterferenceCase{}).Where("id = ?", caseRecord.ID).UpdateColumn("version", 1).Error; err != nil {
		t.Fatalf("reset version: %v", err)
	}
	duplicate := primary
	duplicate.ID = 0
	duplicate.EvidenceHash = &hash
	err := repo.CreateRun(context.Background(), caseRecord.ID, 1, &duplicate, nil, false, 1000, dto.LocalizationBatch{}, actor)
	if err != ErrDuplicateEstimateRun {
		t.Fatalf("expected duplicate run error, got %v", err)
	}
	var count int64
	if err := db.Model(&model.LocalizationEstimate{}).Where("case_id = ? AND evidence_hash = ?", caseRecord.ID, hash).Count(&count).Error; err != nil {
		t.Fatalf("count estimates: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one persisted estimate, got %d", count)
	}
}
