package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"gorm.io/datatypes"

	"spectrum-interference-triangulation/backend/internal/constants"
	"spectrum-interference-triangulation/backend/internal/dto"
	"spectrum-interference-triangulation/backend/internal/localization"
	"spectrum-interference-triangulation/backend/internal/model"
	"spectrum-interference-triangulation/backend/internal/repository"
	"spectrum-interference-triangulation/backend/internal/util"
	"spectrum-interference-triangulation/backend/pkg/api"
)

const (
	localizationBatchWindow  = 30 * time.Minute
	minimumBatchObservations = 3
	minimumBatchStations     = 2
)

type EstimateService struct {
	repo            *repository.EstimateRepository
	observationRepo *repository.ObservationRepository
	caseRepo        *repository.CaseRepository
	conditionLimit  float64
}

type RunResult struct {
	Primary   model.LocalizationEstimate  `json:"primary"`
	Candidate *model.LocalizationEstimate `json:"candidate,omitempty"`
	Reused    bool                        `json:"reused"`
	Batch     *dto.LocalizationBatch      `json:"batch,omitempty"`
}

func NewEstimateService(repo *repository.EstimateRepository, observationRepo *repository.ObservationRepository, caseRepo *repository.CaseRepository, conditionLimit float64) *EstimateService {
	return &EstimateService{repo: repo, observationRepo: observationRepo, caseRepo: caseRepo, conditionLimit: conditionLimit}
}

func (s *EstimateService) List(ctx context.Context, caseID uint) ([]model.LocalizationEstimate, error) {
	return s.repo.List(ctx, caseID)
}

func (s *EstimateService) Get(ctx context.Context, id uint) (model.LocalizationEstimate, error) {
	return s.repo.Get(ctx, id)
}

func (s *EstimateService) BatchPlan(ctx context.Context, caseID uint) (dto.LocalizationBatchPlan, error) {
	caseRecord, err := s.caseRepo.Get(ctx, caseID)
	if err != nil {
		return dto.LocalizationBatchPlan{}, err
	}
	observations, err := s.observationRepo.ListForCase(ctx, caseRecord.ID, true)
	if err != nil {
		return dto.LocalizationBatchPlan{}, err
	}
	plan := buildBatchPlan(caseRecord.FrequencyCenterHz, observations)
	plan.CaseID = caseRecord.ID
	return plan, nil
}

func (s *EstimateService) Run(ctx context.Context, request dto.RunLocalizationRequest, actor repository.Actor) (RunResult, error) {
	if !constants.CanAnalyze(actor.Role) {
		return RunResult{}, api.ErrForbidden
	}
	caseRecord, err := s.caseRepo.Get(ctx, request.CaseID)
	if err != nil {
		return RunResult{}, err
	}
	if caseRecord.CaseStatus != constants.CaseAnalyzing {
		return RunResult{}, api.WithDetails(api.NewError(409, "CASE_NOT_ANALYZING", "只有 analyzing 状态的案例可以运行定位"), map[string]any{"current": caseRecord.CaseStatus})
	}
	observations, err := s.observationRepo.ListForCase(ctx, request.CaseID, true)
	if err != nil {
		return RunResult{}, err
	}
	plan := buildBatchPlan(caseRecord.FrequencyCenterHz, observations)
	plan.CaseID = caseRecord.ID
	batch, err := selectBatch(plan, request.BatchIndex)
	if err != nil {
		return RunResult{}, err
	}
	inputs := batchInputs(batch, observations)
	run, err := localization.SolveWithOutlierCandidate(inputs, s.conditionLimit, request.AllowOutlier)
	if err != nil {
		var degenerate *localization.DegenerateError
		if errors.As(err, &degenerate) {
			return RunResult{}, api.WithDetails(api.NewError(422, "GEOMETRY_DEGENERATE", "方位几何退化，无法形成可信定位点"), map[string]any{
				"condition_number": util.JSONSafeNumber(degenerate.ConditionNumber), "reason": degenerate.Reason,
			})
		}
		if errors.Is(err, localization.ErrInsufficientObservations) {
			return RunResult{}, api.NewError(422, "INSUFFICIENT_OBSERVATIONS", "定位至少需要同一批次内来自两个启用测向站的三条有效观测")
		}
		return RunResult{}, fmt.Errorf("solve localization: %w", err)
	}
	primary, err := buildEstimate(caseRecord.ID, actor.UserID, constants.EstimateComplete, run.Primary, inputs, batch, request.AllowOutlier, s.conditionLimit)
	if err != nil {
		return RunResult{}, err
	}
	var candidate *model.LocalizationEstimate
	if run.Candidate != nil {
		candidateValue, buildErr := buildEstimate(caseRecord.ID, actor.UserID, constants.EstimateCandidate, *run.Candidate, inputs, batch, request.AllowOutlier, s.conditionLimit)
		if buildErr != nil {
			return RunResult{}, buildErr
		}
		candidate = &candidateValue
	}
	if existing, found, lookupErr := s.repo.FindRun(ctx, caseRecord.ID, *primary.EvidenceHash); lookupErr != nil {
		return RunResult{}, lookupErr
	} else if found {
		return RunResult{Primary: existing.Primary, Candidate: existing.Candidate, Reused: true, Batch: &batch}, nil
	}
	if err := s.repo.CreateRun(ctx, caseRecord.ID, caseRecord.Version, &primary, candidate, request.AllowOutlier, s.conditionLimit, batch, actor); err != nil {
		if existing, ok, lookupErr := s.lookupExistingRun(ctx, caseRecord.ID, *primary.EvidenceHash, err); lookupErr != nil {
			return RunResult{}, lookupErr
		} else if ok {
			return RunResult{Primary: existing.Primary, Candidate: existing.Candidate, Reused: true, Batch: &batch}, nil
		}
		return RunResult{}, err
	}
	return RunResult{Primary: primary, Candidate: candidate, Batch: &batch}, nil
}

func (s *EstimateService) lookupExistingRun(ctx context.Context, caseID uint, evidenceHash string, cause error) (repository.EstimateRun, bool, error) {
	if errors.Is(cause, repository.ErrDuplicateEstimateRun) {
		return s.repo.FindRun(ctx, caseID, evidenceHash)
	}
	var appErr *api.Error
	if errors.As(cause, &appErr) && appErr.Code == "CASE_VERSION_CONFLICT" {
		return s.repo.FindRun(ctx, caseID, evidenceHash)
	}
	return repository.EstimateRun{}, false, nil
}

func buildBatchPlan(centerFrequencyHz float64, observations []model.BearingObservation) dto.LocalizationBatchPlan {
	eligible := make([]model.BearingObservation, 0, len(observations))
	unbatched := make([]dto.LocalizationBatchObservation, 0)
	for _, observation := range observations {
		reasons := observationUnavailableReasons(centerFrequencyHz, observation)
		if len(reasons) > 0 {
			unbatched = append(unbatched, batchObservation(observation, false, reasons))
			continue
		}
		eligible = append(eligible, observation)
	}

	batches := make([]dto.LocalizationBatch, 0)
	if len(eligible) > 0 {
		windowStart := eligible[0].ObservedAt.UTC()
		windowEnd := windowStart.Add(localizationBatchWindow)
		current := dto.LocalizationBatch{BatchIndex: 1, WindowStart: windowStart, WindowEnd: windowEnd, Observations: []dto.LocalizationBatchObservation{}}
		for _, observation := range eligible {
			if !observation.ObservedAt.UTC().Before(windowEnd) {
				finalizeBatch(&current)
				batches = append(batches, current)
				windowStart = observation.ObservedAt.UTC()
				windowEnd = windowStart.Add(localizationBatchWindow)
				current = dto.LocalizationBatch{BatchIndex: len(batches) + 1, WindowStart: windowStart, WindowEnd: windowEnd, Observations: []dto.LocalizationBatchObservation{}}
			}
			current.Observations = append(current.Observations, batchObservation(observation, true, nil))
		}
		finalizeBatch(&current)
		batches = append(batches, current)
	}
	return dto.LocalizationBatchPlan{
		WindowMinutes:        int(localizationBatchWindow / time.Minute),
		RequiredObservations: minimumBatchObservations, RequiredStationCount: minimumBatchStations,
		Batches: batches, UnbatchedObservations: unbatched,
	}
}

func observationUnavailableReasons(centerFrequencyHz float64, observation model.BearingObservation) []string {
	reasons := []string{}
	if observation.Quality == constants.QualityExcluded {
		reason := "观测已被人工排除"
		if observation.ExcludedReason != "" {
			reason += "：" + observation.ExcludedReason
		}
		reasons = append(reasons, reason)
	}
	if observation.Station == nil || observation.Station.StationStatus != "active" {
		reasons = append(reasons, "测向站未处于启用状态")
	}
	if math.Abs(centerFrequencyHz-observation.FrequencyHz) > observation.BandwidthHz/2 {
		reasons = append(reasons, "观测频率超出案例中心频率带宽")
	}
	return reasons
}

func batchObservation(observation model.BearingObservation, available bool, reasons []string) dto.LocalizationBatchObservation {
	stationCode := ""
	if observation.Station != nil {
		stationCode = observation.Station.StationCode
	}
	if reasons == nil {
		reasons = []string{}
	}
	return dto.LocalizationBatchObservation{
		ObservationID: observation.ID, StationID: observation.StationID, StationCode: stationCode,
		ObservedAt: observation.ObservedAt.UTC(), Quality: string(observation.Quality),
		Available: available, Reasons: reasons,
	}
}

func finalizeBatch(batch *dto.LocalizationBatch) {
	stationCount := map[uint]struct{}{}
	for _, observation := range batch.Observations {
		stationCount[observation.StationID] = struct{}{}
	}
	batch.Reasons = []string{}
	if len(batch.Observations) < minimumBatchObservations {
		batch.Reasons = append(batch.Reasons, "同一批次有效观测不足三条")
	}
	if len(stationCount) < minimumBatchStations {
		batch.Reasons = append(batch.Reasons, "同一批次有效观测未覆盖至少两个不同测向站")
	}
	batch.Eligible = len(batch.Reasons) == 0
}

func selectBatch(plan dto.LocalizationBatchPlan, requestedIndex int) (dto.LocalizationBatch, error) {
	if requestedIndex == 0 {
		eligible := make([]dto.LocalizationBatch, 0)
		for _, batch := range plan.Batches {
			if batch.Eligible {
				eligible = append(eligible, batch)
			}
		}
		if len(eligible) == 1 {
			return eligible[0], nil
		}
		if len(eligible) == 0 {
			return dto.LocalizationBatch{}, api.NewError(422, "NO_ELIGIBLE_LOCALIZATION_BATCH", "没有满足三观测、两测向站要求的三十分钟批次")
		}
		return dto.LocalizationBatch{}, api.NewError(409, "LOCALIZATION_BATCH_REQUIRED", "存在多个可运行批次，请在请求中指定 batch_index")
	}
	for _, batch := range plan.Batches {
		if batch.BatchIndex == requestedIndex {
			if !batch.Eligible {
				return dto.LocalizationBatch{}, api.WithDetails(api.NewError(422, "LOCALIZATION_BATCH_INELIGIBLE", "该批次不满足定位时间一致性门禁"), map[string]any{
					"batch_index": batch.BatchIndex, "reasons": batch.Reasons,
				})
			}
			return batch, nil
		}
	}
	return dto.LocalizationBatch{}, api.NewError(404, "LOCALIZATION_BATCH_NOT_FOUND", "指定的观测批次不存在")
}

func batchInputs(batch dto.LocalizationBatch, observations []model.BearingObservation) []localization.Input {
	available := map[uint]struct{}{}
	for _, item := range batch.Observations {
		if item.Available {
			available[item.ObservationID] = struct{}{}
		}
	}
	inputs := make([]localization.Input, 0, len(batch.Observations))
	for _, observation := range observations {
		if _, ok := available[observation.ID]; !ok || observation.Station == nil {
			continue
		}
		inputs = append(inputs, localization.Input{
			ObservationID: observation.ID, StationID: observation.StationID,
			StationCode: observation.Station.StationCode,
			Latitude:    observation.Station.Latitude, Longitude: observation.Station.Longitude,
			BearingDeg: observation.CorrectedBearingDeg, AccuracyDeg: observation.Station.AccuracyDeg,
			QualityWeight: constants.QualityWeight(observation.Quality), ObservedAt: observation.ObservedAt.UTC(),
		})
	}
	return inputs
}

func buildEstimate(caseID, userID uint, status string, result localization.Result, inputs []localization.Input, batch dto.LocalizationBatch, allowOutlier bool, conditionLimit float64) (model.LocalizationEstimate, error) {
	usedJSON, err := json.Marshal(result.UsedObservationIDs)
	if err != nil {
		return model.LocalizationEstimate{}, fmt.Errorf("marshal used observation IDs: %w", err)
	}
	outlierJSON, err := json.Marshal(result.OutlierIDs)
	if err != nil {
		return model.LocalizationEstimate{}, fmt.Errorf("marshal outlier IDs: %w", err)
	}
	residualJSON, err := json.Marshal(result.Residuals)
	if err != nil {
		return model.LocalizationEstimate{}, fmt.Errorf("marshal residual evidence: %w", err)
	}
	snapshotJSON, err := json.Marshal(inputs)
	if err != nil {
		return model.LocalizationEstimate{}, fmt.Errorf("marshal localization snapshot: %w", err)
	}
	batchJSON, err := json.Marshal(batch)
	if err != nil {
		return model.LocalizationEstimate{}, fmt.Errorf("marshal localization batch snapshot: %w", err)
	}
	if math.IsNaN(result.Point.Latitude) || math.IsNaN(result.Point.Longitude) {
		return model.LocalizationEstimate{}, api.NewError(422, "INVALID_ESTIMATE", "定位算法产生了无效坐标")
	}
	batchStart := batch.WindowStart.UTC()
	batchEnd := batch.WindowEnd.UTC()
	hash := evidenceHash(batch, inputs, result.UsedObservationIDs, result.OutlierIDs, status, allowOutlier, conditionLimit)
	return model.LocalizationEstimate{
		CaseID: caseID, BatchIndex: batch.BatchIndex, BatchStart: &batchStart, BatchEnd: &batchEnd,
		AlgorithmVersion: localization.AlgorithmVersion,
		Latitude:         result.Point.Latitude, Longitude: result.Point.Longitude,
		UncertaintyRadiusM: result.UncertaintyRadiusM, ResidualDeg: result.ResidualDeg,
		ConditionNumber: result.ConditionNumber, ConditionLimit: conditionLimit, GeometryDegenerate: false,
		OutlierRequested:       allowOutlier,
		UsedObservationIDsJSON: datatypes.JSON(usedJSON), OutlierIDsJSON: datatypes.JSON(outlierJSON),
		ResidualsJSON: datatypes.JSON(residualJSON), InputSnapshotJSON: datatypes.JSON(snapshotJSON),
		BatchSnapshotJSON: datatypes.JSON(batchJSON), EvidenceHash: &hash,
		EstimateStatus: status, CreatedBy: userID,
	}, nil
}

func evidenceHash(batch dto.LocalizationBatch, inputs []localization.Input, usedIDs, outlierIDs []uint, status string, allowOutlier bool, conditionLimit float64) string {
	used := make(map[uint]struct{}, len(usedIDs))
	for _, id := range usedIDs {
		used[id] = struct{}{}
	}
	evidence := make([]localization.Input, 0, len(usedIDs))
	for _, input := range inputs {
		if _, ok := used[input.ObservationID]; ok {
			evidence = append(evidence, input)
		}
	}
	outlierIDValues := append([]uint(nil), outlierIDs...)
	sort.Slice(outlierIDValues, func(i, j int) bool { return outlierIDValues[i] < outlierIDValues[j] })
	sort.Slice(evidence, func(i, j int) bool { return evidence[i].ObservationID < evidence[j].ObservationID })
	payload := struct {
		AlgorithmVersion string               `json:"algorithm_version"`
		BatchIndex       int                  `json:"batch_index"`
		WindowStart      time.Time            `json:"window_start"`
		WindowEnd        time.Time            `json:"window_end"`
		ConditionLimit   float64              `json:"condition_limit"`
		AllowOutlier     bool                 `json:"allow_outlier"`
		OutlierIDs       []uint               `json:"outlier_ids"`
		Status           string               `json:"status"`
		Inputs           []localization.Input `json:"inputs"`
	}{AlgorithmVersion: localization.AlgorithmVersion, BatchIndex: batch.BatchIndex, WindowStart: batch.WindowStart.UTC(), WindowEnd: batch.WindowEnd.UTC(), ConditionLimit: conditionLimit, AllowOutlier: allowOutlier, OutlierIDs: outlierIDValues, Status: status, Inputs: evidence}
	data, _ := json.Marshal(payload)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
