package service

import (
	"testing"
	"time"

	"spectrum-interference-triangulation/backend/internal/constants"
	"spectrum-interference-triangulation/backend/internal/model"
)

func TestBuildBatchPlanEnforcesThirtyMinuteWindowAndStationCoverage(t *testing.T) {
	start := time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)
	station := func(id uint, status string) *model.ReceiverStation {
		return &model.ReceiverStation{ID: id, StationCode: "RX", StationStatus: status, AccuracyDeg: 1}
	}
	observations := []model.BearingObservation{
		{ID: 1, StationID: 1, CaseID: 9, FrequencyHz: 100, BandwidthHz: 20, ObservedAt: start, Quality: constants.QualityGood, Station: station(1, "active")},
		{ID: 2, StationID: 2, CaseID: 9, FrequencyHz: 100, BandwidthHz: 20, ObservedAt: start.Add(10 * time.Minute), Quality: constants.QualityGood, Station: station(2, "active")},
		{ID: 3, StationID: 3, CaseID: 9, FrequencyHz: 100, BandwidthHz: 20, ObservedAt: start.Add(20 * time.Minute), Quality: constants.QualityFair, Station: station(3, "active")},
		{ID: 4, StationID: 1, CaseID: 9, FrequencyHz: 100, BandwidthHz: 20, ObservedAt: start.Add(30 * time.Minute), Quality: constants.QualityGood, Station: station(1, "active")},
		{ID: 5, StationID: 2, CaseID: 9, FrequencyHz: 100, BandwidthHz: 20, ObservedAt: start.Add(40 * time.Minute), Quality: constants.QualityGood, Station: station(2, "active")},
		{ID: 6, StationID: 1, CaseID: 9, FrequencyHz: 100, BandwidthHz: 20, ObservedAt: start.Add(50 * time.Minute), Quality: constants.QualityExcluded, ExcludedReason: "人工复核异常", Station: station(1, "active")},
	}

	plan := buildBatchPlan(100, observations)
	if len(plan.Batches) != 2 {
		t.Fatalf("expected two batches, got %d", len(plan.Batches))
	}
	if !plan.Batches[0].Eligible {
		t.Fatalf("first batch should be eligible: %v", plan.Batches[0].Reasons)
	}
	if plan.Batches[1].Eligible {
		t.Fatal("second batch should not be eligible")
	}
	if len(plan.Batches[0].Observations) != 3 || len(plan.Batches[1].Observations) != 2 {
		t.Fatalf("unexpected batch sizes: %d, %d", len(plan.Batches[0].Observations), len(plan.Batches[1].Observations))
	}
	if got := plan.UnbatchedObservations[0].ObservationID; got != 6 {
		t.Fatalf("expected excluded observation retained outside batches, got %d", got)
	}
}

func TestBuildBatchPlanRejectsSingleStationBatch(t *testing.T) {
	start := time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)
	active := &model.ReceiverStation{ID: 1, StationStatus: "active", AccuracyDeg: 1}
	observations := []model.BearingObservation{
		{ID: 1, StationID: 1, FrequencyHz: 100, BandwidthHz: 20, ObservedAt: start, Quality: constants.QualityGood, Station: active},
		{ID: 2, StationID: 1, FrequencyHz: 100, BandwidthHz: 20, ObservedAt: start.Add(time.Minute), Quality: constants.QualityGood, Station: active},
		{ID: 3, StationID: 1, FrequencyHz: 100, BandwidthHz: 20, ObservedAt: start.Add(2 * time.Minute), Quality: constants.QualityGood, Station: active},
	}
	plan := buildBatchPlan(100, observations)
	if plan.Batches[0].Eligible {
		t.Fatal("batch from one station must not be eligible")
	}
	if len(plan.Batches[0].Reasons) != 1 {
		t.Fatalf("expected station coverage reason, got %v", plan.Batches[0].Reasons)
	}
}
