package service

import (
	"testing"
	"time"

	"spectrum-interference-triangulation/backend/internal/constants"
	"spectrum-interference-triangulation/backend/internal/localization"
	"spectrum-interference-triangulation/backend/internal/model"
)

func observationFixture(id, stationID uint, stationCode string, observedAt time.Time, quality constants.ObservationQuality) model.BearingObservation {
	return model.BearingObservation{
		ID: id, StationID: stationID, CaseID: 1,
		BearingDeg: 90, CorrectedBearingDeg: 90, SignalDBM: -70,
		FrequencyHz: 433920000, BandwidthHz: 25000, ObservedAt: observedAt,
		Quality: quality,
		Station: &model.ReceiverStation{ID: stationID, StationCode: stationCode, StationStatus: "active", AccuracyDeg: 1},
	}
}

func TestBuildBatchPlanUsesThirtyMinuteWindowsFromEarliest(t *testing.T) {
	origin := time.Date(2026, 9, 22, 8, 0, 0, 0, time.UTC)
	observations := []model.BearingObservation{
		observationFixture(1, 1, "A", origin, constants.QualityGood),
		observationFixture(2, 2, "B", origin.Add(15*time.Minute), constants.QualityGood),
		observationFixture(3, 3, "C", origin.Add(29*time.Minute+59*time.Second), constants.QualityFair),
		observationFixture(4, 1, "A", origin.Add(30*time.Minute), constants.QualityGood),
	}
	plan := buildBatchPlan(observations, 433920000)
	if len(plan.Batches) != 2 {
		t.Fatalf("expected 2 batches, got %d", len(plan.Batches))
	}
	if len(plan.Batches[0].Members) != 3 {
		t.Fatalf("expected first batch to hold 3 observations, got %d", len(plan.Batches[0].Members))
	}
	if len(plan.Batches[1].Members) != 1 {
		t.Fatalf("expected second batch to hold 1 observation, got %d", len(plan.Batches[1].Members))
	}
	if plan.Batches[1].Index != 1 {
		t.Fatalf("expected contiguous batch index 1, got %d", plan.Batches[1].Index)
	}
	if got := plan.Batches[0].WindowEnd.Sub(plan.Batches[0].WindowStart); got != 30*time.Minute {
		t.Fatalf("expected 30 minute window, got %s", got)
	}
}

func TestBatchGateRequiresThreeObservationsFromTwoStations(t *testing.T) {
	origin := time.Date(2026, 9, 22, 8, 0, 0, 0, time.UTC)
	tests := []struct {
		name         string
		observations []model.BearingObservation
		wantReasons  []string
	}{
		{
			name: "two observations from two stations",
			observations: []model.BearingObservation{
				observationFixture(1, 1, "A", origin, constants.QualityGood),
				observationFixture(2, 2, "B", origin.Add(time.Minute), constants.QualityGood),
			},
			wantReasons: []string{GateReasonObservationCount},
		},
		{
			name: "three observations from one station",
			observations: []model.BearingObservation{
				observationFixture(1, 1, "A", origin, constants.QualityGood),
				observationFixture(2, 1, "A", origin.Add(time.Minute), constants.QualityGood),
				observationFixture(3, 1, "A", origin.Add(2*time.Minute), constants.QualityGood),
			},
			wantReasons: []string{GateReasonStationCount},
		},
		{
			name: "three observations from two stations passes",
			observations: []model.BearingObservation{
				observationFixture(1, 1, "A", origin, constants.QualityGood),
				observationFixture(2, 1, "A", origin.Add(time.Minute), constants.QualityGood),
				observationFixture(3, 2, "B", origin.Add(2*time.Minute), constants.QualityGood),
			},
			wantReasons: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := buildBatchPlan(tt.observations, 433920000)
			if len(plan.Batches) != 1 {
				t.Fatalf("expected 1 batch, got %d", len(plan.Batches))
			}
			reasons := plan.Batches[0].gateReasons()
			if len(reasons) != len(tt.wantReasons) {
				t.Fatalf("expected reasons %v, got %v", tt.wantReasons, reasons)
			}
			for index, reason := range tt.wantReasons {
				if reasons[index] != reason {
					t.Fatalf("expected reason %s, got %s", reason, reasons[index])
				}
			}
		})
	}
}

func TestBuildBatchPlanKeepsExcludedObservationsOutOfBatches(t *testing.T) {
	origin := time.Date(2026, 9, 22, 8, 0, 0, 0, time.UTC)
	observations := []model.BearingObservation{
		observationFixture(1, 1, "A", origin, constants.QualityExcluded),
		observationFixture(2, 2, "B", origin.Add(time.Minute), constants.QualityGood),
	}
	plan := buildBatchPlan(observations, 433920000)
	if len(plan.Batches) != 1 || len(plan.Batches[0].Members) != 1 {
		t.Fatalf("excluded observation must not join a batch: %+v", plan.Batches)
	}
	if len(plan.Excluded) != 1 || plan.Excluded[0].ID != 1 {
		t.Fatalf("expected excluded observation retained with reason, got %+v", plan.Excluded)
	}
	if got := plan.Reasons[1]; len(got) != 1 || got[0] != ExcludeReasonQualityExcluded {
		t.Fatalf("unexpected exclusion reasons: %v", got)
	}
}

func TestBuildBatchPlanExcludesInactiveStationAndFrequencyMismatch(t *testing.T) {
	origin := time.Date(2026, 9, 22, 8, 0, 0, 0, time.UTC)
	inactive := observationFixture(1, 1, "A", origin, constants.QualityGood)
	inactive.Station.StationStatus = "maintenance"
	mismatched := observationFixture(2, 2, "B", origin.Add(time.Minute), constants.QualityGood)
	mismatched.FrequencyHz = 900000000
	plan := buildBatchPlan([]model.BearingObservation{inactive, mismatched}, 433920000)
	if len(plan.Batches) != 0 {
		t.Fatalf("expected no runnable evidence batches, got %d", len(plan.Batches))
	}
	if got := plan.Reasons[1]; len(got) != 1 || got[0] != ExcludeReasonStationInactive {
		t.Fatalf("unexpected inactive station reasons: %v", got)
	}
	if got := plan.Reasons[2]; len(got) != 1 || got[0] != ExcludeReasonFrequency {
		t.Fatalf("unexpected frequency reasons: %v", got)
	}
}

func TestCrossBatchObservationsDoNotJoinEstimate(t *testing.T) {
	origin := time.Date(2026, 9, 22, 8, 0, 0, 0, time.UTC)
	observations := []model.BearingObservation{
		observationFixture(1, 1, "A", origin, constants.QualityGood),
		observationFixture(2, 2, "B", origin.Add(time.Minute), constants.QualityGood),
		observationFixture(3, 3, "C", origin.Add(2*time.Minute), constants.QualityGood),
		// 第四十条观测落在第二个窗口，保留在案例中但不得参与首个批次。
		observationFixture(4, 1, "A", origin.Add(45*time.Minute), constants.QualityGood),
	}
	plan := buildBatchPlan(observations, 433920000)
	batch, err := selectBatch(plan, nil)
	if err != nil {
		t.Fatalf("expected earliest runnable batch selected: %v", err)
	}
	if len(batch.Members) != 3 {
		t.Fatalf("expected exactly 3 batch members, got %d", len(batch.Members))
	}
}

func TestSelectBatchRejectsGateFailureWithDetails(t *testing.T) {
	origin := time.Date(2026, 9, 22, 8, 0, 0, 0, time.UTC)
	observations := []model.BearingObservation{
		observationFixture(1, 1, "A", origin, constants.QualityGood),
		observationFixture(2, 2, "B", origin.Add(time.Minute), constants.QualityGood),
	}
	plan := buildBatchPlan(observations, 433920000)
	if _, err := selectBatch(plan, nil); err == nil {
		t.Fatal("expected gate failure for two-observation batch")
	}
	if _, err := selectBatch(plan, ptrInt(5)); err == nil {
		t.Fatal("expected invalid batch index error")
	}
}

func TestRunSignatureIsStableAcrossInputOrder(t *testing.T) {
	origin := time.Date(2026, 9, 22, 8, 0, 0, 0, time.UTC)
	observations := []model.BearingObservation{
		observationFixture(1, 1, "A", origin, constants.QualityGood),
		observationFixture(2, 2, "B", origin.Add(time.Minute), constants.QualityGood),
		observationFixture(3, 3, "C", origin.Add(2*time.Minute), constants.QualityGood),
	}
	plan := buildBatchPlan(observations, 433920000)
	inputs := batchToInputs(plan.Batches[0])
	first := runSignature(inputs, plan.Batches[0], true, 1000)
	// 打乱输入顺序后指纹必须保持一致。
	shuffled := make([]localization.Input, len(inputs))
	copy(shuffled, inputs)
	shuffled[0], shuffled[2] = shuffled[2], shuffled[0]
	second := runSignature(shuffled, plan.Batches[0], true, 1000)
	if first != second {
		t.Fatal("run signature must not depend on input order")
	}
	// 改变离群开关必须产生不同指纹。
	third := runSignature(inputs, plan.Batches[0], false, 1000)
	if first == third {
		t.Fatal("run signature must change when allow_outlier changes")
	}
}

func TestRebatchAfterRescheduleMovesObservationWindow(t *testing.T) {
	origin := time.Date(2026, 9, 22, 8, 0, 0, 0, time.UTC)
	observations := []model.BearingObservation{
		observationFixture(1, 1, "A", origin, constants.QualityGood),
		observationFixture(2, 2, "B", origin.Add(time.Minute), constants.QualityGood),
		observationFixture(3, 3, "C", origin.Add(2*time.Minute), constants.QualityGood),
		observationFixture(4, 1, "A", origin.Add(45*time.Minute), constants.QualityGood),
	}
	plan := buildBatchPlan(observations, 433920000)
	if len(plan.Batches) != 2 {
		t.Fatalf("expected 2 batches before reschedule, got %d", len(plan.Batches))
	}
	// 将跨批次观测改期回首个窗口，重新分批后应合并为单一批次。
	observations[3].ObservedAt = origin.Add(5 * time.Minute)
	rebatched := buildBatchPlan(observations, 433920000)
	if len(rebatched.Batches) != 1 || len(rebatched.Batches[0].Members) != 4 {
		t.Fatalf("expected single rebatch with 4 members, got %+v", rebatched.Batches)
	}
}

func ptrInt(value int) *int { return &value }
