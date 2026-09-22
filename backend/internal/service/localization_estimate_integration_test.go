package service

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"spectrum-interference-triangulation/backend/internal/constants"
	"spectrum-interference-triangulation/backend/internal/dto"
	"spectrum-interference-triangulation/backend/internal/model"
	"spectrum-interference-triangulation/backend/internal/repository"
)

var integrationDBCounter int64

func newIntegrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:it-%d?mode=memory&cache=shared", atomic.AddInt64(&integrationDBCounter, 1))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&model.User{}, &model.ReceiverStation{}, &model.InterferenceCase{},
		&model.BearingObservation{}, &model.LocalizationEstimate{}, &model.AuditEvent{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	})
	return db
}

func integrationActor() repository.Actor {
	return repository.Actor{UserID: 7, Email: "analyst@test.local", Role: constants.RoleAnalyst, RequestID: "test-request"}
}

func seedIntegrationCase(t *testing.T, db *gorm.DB, spreadAcrossBatches bool) (model.InterferenceCase, []model.ReceiverStation) {
	t.Helper()
	now := time.Date(2026, 9, 22, 8, 0, 0, 0, time.UTC)
	stations := []model.ReceiverStation{
		{StationCode: "RX-W", Name: "W", Latitude: 31.23, Longitude: 121.45, AccuracyDeg: 1, StationStatus: "active", CalibratedAt: &now},
		{StationCode: "RX-S", Name: "S", Latitude: 31.21, Longitude: 121.47, AccuracyDeg: 1, StationStatus: "active", CalibratedAt: &now},
		{StationCode: "RX-E", Name: "E", Latitude: 31.23, Longitude: 121.49, AccuracyDeg: 1, StationStatus: "active", CalibratedAt: &now},
	}
	if err := db.Create(&stations).Error; err != nil {
		t.Fatalf("create stations: %v", err)
	}
	caseRecord := model.InterferenceCase{
		CaseCode: "RF-T-001", Title: "integration case", FrequencyCenterHz: 433920000,
		CaseStatus: constants.CaseAnalyzing, Priority: "normal", OpenedBy: 7, Version: 1,
	}
	if err := db.Create(&caseRecord).Error; err != nil {
		t.Fatalf("create case: %v", err)
	}
	offsets := []time.Duration{0, time.Minute, 2 * time.Minute}
	if spreadAcrossBatches {
		offsets = []time.Duration{0, time.Minute, 45 * time.Minute}
	}
	bearings := []float64{90, 0, 270}
	for index := range stations {
		observation := model.BearingObservation{
			StationID: stations[index].ID, CaseID: caseRecord.ID, BearingDeg: bearings[index],
			CorrectedBearingDeg: bearings[index], SignalDBM: -70, FrequencyHz: 433920000, BandwidthHz: 25000,
			ObservedAt: now.Add(offsets[index]), Quality: constants.QualityGood, CreatedBy: 1,
		}
		if err := db.Create(&observation).Error; err != nil {
			t.Fatalf("create observation: %v", err)
		}
	}
	return caseRecord, stations
}

func newEstimateService(db *gorm.DB) *EstimateService {
	return NewEstimateService(
		repository.NewEstimateRepository(db),
		repository.NewObservationRepository(db),
		repository.NewCaseRepository(db),
		1000,
	)
}

func TestRunCreatesSingleResultForRepeatedBatch(t *testing.T) {
	db := newIntegrationDB(t)
	caseRecord, _ := seedIntegrationCase(t, db, false)
	service := newEstimateService(db)
	ctx := context.Background()

	first, err := service.Run(ctx, dto.RunLocalizationRequest{CaseID: caseRecord.ID, AllowOutlier: true}, integrationActor())
	if err != nil {
		t.Fatalf("first run failed: %v", err)
	}
	if first.Reused {
		t.Fatal("first run should create a new result")
	}
	if first.Primary.BatchIndex == nil || *first.Primary.BatchIndex != 0 {
		t.Fatalf("expected batch 0 snapshot, got %v", first.Primary.BatchIndex)
	}
	if len(first.Primary.BatchObservationIDsJSON) == 0 {
		t.Fatal("expected batch observation evidence snapshot")
	}

	second, err := service.Run(ctx, dto.RunLocalizationRequest{CaseID: caseRecord.ID, AllowOutlier: true}, integrationActor())
	if err != nil {
		t.Fatalf("second run failed: %v", err)
	}
	if !second.Reused || second.Primary.ID != first.Primary.ID {
		t.Fatal("repeat run must reuse the single existing result")
	}

	var count int64
	if err := db.Model(&model.LocalizationEstimate{}).Count(&count).Error; err != nil {
		t.Fatalf("count estimates: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly one persisted estimate, got %d", count)
	}
}

func TestRunRejectsBatchThatFailsGate(t *testing.T) {
	db := newIntegrationDB(t)
	caseRecord, _ := seedIntegrationCase(t, db, true)
	service := newEstimateService(db)

	plan, err := service.PreviewBatches(context.Background(), caseRecord.ID)
	if err != nil {
		t.Fatalf("preview batches: %v", err)
	}
	if len(plan.Batches) != 2 {
		t.Fatalf("expected two windows, got %d", len(plan.Batches))
	}
	firstBatch := plan.Batches[0]
	if firstBatch.Runnable || len(firstBatch.GateReasons) == 0 {
		t.Fatalf("first batch must fail gate: %+v", firstBatch)
	}

	_, err = service.Run(context.Background(), dto.RunLocalizationRequest{CaseID: caseRecord.ID, AllowOutlier: false}, integrationActor())
	if err == nil {
		t.Fatal("expected run to be rejected when no runnable batch exists")
	}
	_, err = service.Run(context.Background(), dto.RunLocalizationRequest{CaseID: caseRecord.ID, BatchIndex: ptrInt(0)}, integrationActor())
	if err == nil {
		t.Fatal("expected explicit selection of a blocked batch to be rejected")
	}
}

func TestConcurrentRunsProduceSingleResult(t *testing.T) {
	db := newIntegrationDB(t)
	caseRecord, _ := seedIntegrationCase(t, db, false)
	service := newEstimateService(db)

	const goroutines = 6
	results := make(chan RunResult, goroutines)
	errs := make(chan error, goroutines)
	barrier := make(chan struct{})
	for i := 0; i < goroutines; i++ {
		go func() {
			<-barrier
			result, err := service.Run(context.Background(), dto.RunLocalizationRequest{CaseID: caseRecord.ID, AllowOutlier: false}, integrationActor())
			if err != nil {
				errs <- err
				return
			}
			results <- result
		}()
	}
	close(barrier)

	successes, reused := 0, 0
	for i := 0; i < goroutines; i++ {
		select {
		case result := <-results:
			successes++
			if result.Reused {
				reused++
			}
		case err := <-errs:
			t.Fatalf("concurrent run must converge to one result, got error: %v", err)
		}
	}
	if successes != goroutines || reused != goroutines-1 {
		t.Fatalf("expected 1 created and %d reused, got reused=%d", goroutines-1, reused)
	}
	var count int64
	if err := db.Model(&model.LocalizationEstimate{}).Count(&count).Error; err != nil {
		t.Fatalf("count estimates: %v", err)
	}
	if count != 1 {
		t.Fatalf("concurrent runs must persist exactly one result, got %d", count)
	}
}
