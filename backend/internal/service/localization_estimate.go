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
	"gorm.io/gorm"

	"spectrum-interference-triangulation/backend/internal/constants"
	"spectrum-interference-triangulation/backend/internal/dto"
	"spectrum-interference-triangulation/backend/internal/localization"
	"spectrum-interference-triangulation/backend/internal/model"
	"spectrum-interference-triangulation/backend/internal/repository"
	"spectrum-interference-triangulation/backend/internal/util"
	"spectrum-interference-triangulation/backend/pkg/api"
)

// BatchWindowMinutes 是定位批次时间一致性门禁的采集窗口长度。
const BatchWindowMinutes = 30

// 批次门禁不满足原因，前端按原因码本地化展示。
const (
	GateReasonObservationCount = "OBSERVATION_COUNT_BELOW_3"
	GateReasonStationCount     = "STATION_COUNT_BELOW_2"
)

// 观测无法成为有效采集证据的原因。
const (
	ExcludeReasonQualityExcluded = "OBSERVATION_QUALITY_EXCLUDED"
	ExcludeReasonStationInactive = "STATION_NOT_ACTIVE"
	ExcludeReasonFrequency       = "FREQUENCY_OUT_OF_BAND"
)

type EstimateService struct {
	repo            *repository.EstimateRepository
	observationRepo *repository.ObservationRepository
	caseRepo        *repository.CaseRepository
	conditionLimit  float64
}

type RunResult struct {
	Primary          model.LocalizationEstimate  `json:"primary"`
	Candidate        *model.LocalizationEstimate `json:"candidate,omitempty"`
	BatchIndex       int                         `json:"batch_index"`
	BatchWindowStart time.Time                   `json:"batch_window_start"`
	BatchWindowEnd   time.Time                   `json:"batch_window_end"`
	// Reused 为 true 时表示该证据批次已有结果，本次未生成新结果。
	Reused bool `json:"reused"`
}

// batchMember 是一条进入分批判定的有效观测及其解析后的站点信息。
type batchMember struct {
	Observation model.BearingObservation
	Station     model.ReceiverStation
}

// observationBatch 描述一个非空 30 分钟采集窗口。
type observationBatch struct {
	Index       int
	WindowStart time.Time
	WindowEnd   time.Time
	Members     []batchMember
}

// batchPlan 是案例全部观测的分批结果，含被排除观测的证据。
type batchPlan struct {
	Batches  []observationBatch
	Excluded []model.BearingObservation
	Reasons  map[uint][]string
}

// classifyObservation 返回观测被排除于有效采集证据之外的原因列表，为空表示有效。
func classifyObservation(observation model.BearingObservation, centerFrequency float64) []string {
	reasons := make([]string, 0, 3)
	if observation.Quality == constants.QualityExcluded {
		reasons = append(reasons, ExcludeReasonQualityExcluded)
	}
	if observation.Station == nil || observation.Station.StationStatus != "active" {
		reasons = append(reasons, ExcludeReasonStationInactive)
	}
	if math.Abs(observation.FrequencyHz-centerFrequency) > observation.BandwidthHz/2 {
		reasons = append(reasons, ExcludeReasonFrequency)
	}
	return reasons
}

// buildBatchPlan 以最早有效观测为起点，按 30 分钟采集窗口对有效观测分批。
// 被排除、站点停用或频率失配的观测保留在案例中，但不参与分批与估计。
func buildBatchPlan(observations []model.BearingObservation, centerFrequency float64) batchPlan {
	members := make([]batchMember, 0, len(observations))
	excluded := make([]model.BearingObservation, 0)
	reasons := make(map[uint][]string, len(observations))
	for _, observation := range observations {
		issueReasons := classifyObservation(observation, centerFrequency)
		if len(issueReasons) > 0 {
			excluded = append(excluded, observation)
			reasons[observation.ID] = issueReasons
			continue
		}
		members = append(members, batchMember{Observation: observation, Station: *observation.Station})
	}
	sort.Slice(members, func(i, j int) bool {
		if members[i].Observation.ObservedAt.Equal(members[j].Observation.ObservedAt) {
			return members[i].Observation.ID < members[j].Observation.ID
		}
		return members[i].Observation.ObservedAt.Before(members[j].Observation.ObservedAt)
	})
	plan := batchPlan{Batches: []observationBatch{}, Excluded: excluded, Reasons: reasons}
	if len(members) == 0 {
		return plan
	}
	origin := members[0].Observation.ObservedAt.UTC()
	window := time.Duration(BatchWindowMinutes) * time.Minute
	groups := make(map[int][]batchMember)
	maxIndex := 0
	for _, member := range members {
		elapsed := member.Observation.ObservedAt.UTC().Sub(origin)
		index := int(math.Floor(float64(elapsed) / float64(window)))
		if index < 0 {
			index = 0
		}
		groups[index] = append(groups[index], member)
		if index > maxIndex {
			maxIndex = index
		}
	}
	for index := 0; index <= maxIndex; index++ {
		group := groups[index]
		if len(group) == 0 {
			continue
		}
		start := origin.Add(time.Duration(index) * window)
		plan.Batches = append(plan.Batches, observationBatch{
			Index:       len(plan.Batches),
			WindowStart: start,
			WindowEnd:   start.Add(window),
			Members:     group,
		})
	}
	return plan
}

// gateReasons 返回批次不满足定位门禁的原因；为空表示可运行。
// 门禁要求同一批次内至少三条有效观测且来自至少两个不同测向站。
func (b observationBatch) gateReasons() []string {
	reasons := make([]string, 0, 2)
	if len(b.Members) < 3 {
		reasons = append(reasons, GateReasonObservationCount)
	}
	stations := make(map[uint]struct{}, len(b.Members))
	for _, member := range b.Members {
		stations[member.Station.ID] = struct{}{}
	}
	if len(stations) < 2 {
		reasons = append(reasons, GateReasonStationCount)
	}
	return reasons
}

func (b observationBatch) runnable() bool {
	return len(b.gateReasons()) == 0
}

// runSignature 生成证据批次的确定性指纹，用于重复与并发运行去重。
func runSignature(inputs []localization.Input, batch observationBatch, allowOutlier bool, conditionLimit float64) string {
	type signatureInput struct {
		ObservationID uint    `json:"id"`
		BearingDeg    float64 `json:"bearing"`
		AccuracyDeg   float64 `json:"accuracy"`
		QualityWeight float64 `json:"weight"`
		Latitude      float64 `json:"lat"`
		Longitude     float64 `json:"lon"`
	}
	signatureInputs := make([]signatureInput, 0, len(inputs))
	for _, input := range inputs {
		signatureInputs = append(signatureInputs, signatureInput{
			ObservationID: input.ObservationID, BearingDeg: input.BearingDeg,
			AccuracyDeg: input.AccuracyDeg, QualityWeight: input.QualityWeight,
			Latitude: input.Latitude, Longitude: input.Longitude,
		})
	}
	sort.Slice(signatureInputs, func(i, j int) bool {
		return signatureInputs[i].ObservationID < signatureInputs[j].ObservationID
	})
	ids := make([]uint, 0, len(inputs))
	for _, input := range signatureInputs {
		ids = append(ids, input.ObservationID)
	}
	payload := struct {
		Algorithm      string           `json:"algorithm"`
		WindowStart    time.Time        `json:"window_start"`
		WindowEnd      time.Time        `json:"window_end"`
		ObservationIDs []uint           `json:"observation_ids"`
		Inputs         []signatureInput `json:"inputs"`
		AllowOutlier   bool             `json:"allow_outlier"`
		ConditionLimit float64          `json:"condition_limit"`
	}{
		Algorithm: localization.AlgorithmVersion, WindowStart: batch.WindowStart.UTC(),
		WindowEnd: batch.WindowEnd.UTC(), ObservationIDs: ids, Inputs: signatureInputs,
		AllowOutlier: allowOutlier, ConditionLimit: conditionLimit,
	}
	encoded, _ := json.Marshal(payload)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

// batchToInputs 将一个批次的观测转换为定位求解输入。
func batchToInputs(batch observationBatch) []localization.Input {
	inputs := make([]localization.Input, 0, len(batch.Members))
	for _, member := range batch.Members {
		inputs = append(inputs, localization.Input{
			ObservationID: member.Observation.ID, StationCode: member.Station.StationCode,
			Latitude: member.Station.Latitude, Longitude: member.Station.Longitude,
			BearingDeg: member.Observation.CorrectedBearingDeg, AccuracyDeg: member.Station.AccuracyDeg,
			QualityWeight: constants.QualityWeight(member.Observation.Quality),
		})
	}
	return inputs
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
	plan := buildBatchPlan(observations, caseRecord.FrequencyCenterHz)
	batch, err := selectBatch(plan, request.BatchIndex)
	if err != nil {
		return RunResult{}, err
	}
	inputs := batchToInputs(batch)
	signature := runSignature(inputs, batch, request.AllowOutlier, s.conditionLimit)
	// 顺序重复运行：证据批次指纹已有结果时直接复用，不生成新结果。
	if existing, findErr := s.repo.FindBySignature(ctx, caseRecord.ID, signature); findErr != nil {
		return RunResult{}, findErr
	} else if existing != nil {
		return RunResult{
			Primary: *existing, Candidate: nil, BatchIndex: batch.Index,
			BatchWindowStart: batch.WindowStart, BatchWindowEnd: batch.WindowEnd, Reused: true,
		}, nil
	}
	run, err := localization.SolveWithOutlierCandidate(inputs, s.conditionLimit, request.AllowOutlier)
	if err != nil {
		var degenerate *localization.DegenerateError
		if errors.As(err, &degenerate) {
			return RunResult{}, api.WithDetails(api.NewError(422, "GEOMETRY_DEGENERATE", "方位几何退化，无法形成可信定位点"), map[string]any{
				"condition_number": util.JSONSafeNumber(degenerate.ConditionNumber), "reason": degenerate.Reason,
				"batch_index": batch.Index,
			})
		}
		if errors.Is(err, localization.ErrInsufficientObservations) {
			return RunResult{}, api.NewError(422, "INSUFFICIENT_OBSERVATIONS", "定位至少需要两条来自启用测向站的有效观测")
		}
		return RunResult{}, fmt.Errorf("solve localization: %w", err)
	}
	batchIDs := make([]uint, 0, len(inputs))
	for _, input := range inputs {
		batchIDs = append(batchIDs, input.ObservationID)
	}
	primary, err := buildEstimate(caseRecord.ID, actor.UserID, constants.EstimateComplete, run.Primary, inputs, signature, batch, batchIDs)
	if err != nil {
		return RunResult{}, err
	}
	var candidate *model.LocalizationEstimate
	if run.Candidate != nil {
		candidateValue, buildErr := buildEstimate(caseRecord.ID, actor.UserID, constants.EstimateCandidate, *run.Candidate, inputs, "", batch, batchIDs)
		if buildErr != nil {
			return RunResult{}, buildErr
		}
		candidate = &candidateValue
	}
	if err := s.repo.CreateRun(ctx, caseRecord.ID, caseRecord.Version, &primary, candidate, request.AllowOutlier, s.conditionLimit, actor); err != nil {
		// 并发重复运行：乐观锁冲突或唯一指纹冲突都意味着另一请求正在或已落库同一份结果，复用已有结果。
		var appErr *api.Error
		if errors.Is(err, gorm.ErrDuplicatedKey) || (errors.As(err, &appErr) && appErr.Code == "CASE_VERSION_CONFLICT") {
			if existing, findErr := s.repo.FindBySignature(ctx, caseRecord.ID, signature); findErr == nil && existing != nil {
				return RunResult{
					Primary: *existing, BatchIndex: batch.Index,
					BatchWindowStart: batch.WindowStart, BatchWindowEnd: batch.WindowEnd, Reused: true,
				}, nil
			}
		}
		return RunResult{}, err
	}
	return RunResult{
		Primary: primary, Candidate: candidate, BatchIndex: batch.Index,
		BatchWindowStart: batch.WindowStart, BatchWindowEnd: batch.WindowEnd, Reused: false,
	}, nil
}

// selectBatch 按请求选择目标批次并强制时间一致性门禁。
func selectBatch(plan batchPlan, requested *int) (observationBatch, error) {
	if len(plan.Batches) == 0 {
		return observationBatch{}, api.WithDetails(api.NewError(422, "BATCH_GATE_FAILED", "案例没有满足分批条件的有效观测"), map[string]any{
			"window_minutes": BatchWindowMinutes,
		})
	}
	index := -1
	if requested != nil {
		index = *requested
		if index < 0 || index >= len(plan.Batches) {
			return observationBatch{}, api.WithDetails(api.NewError(400, "BATCH_INDEX_INVALID", "指定的批次不存在，案例已按当前观测重新分批"), map[string]any{
				"requested":   index,
				"batch_count": len(plan.Batches),
			})
		}
	} else {
		for _, batch := range plan.Batches {
			if batch.runnable() {
				index = batch.Index
				break
			}
		}
	}
	if index < 0 {
		return observationBatch{}, api.WithDetails(api.NewError(422, "BATCH_NO_RUNNABLE", "没有批次同时满足三条有效观测且来自至少两个测向站"), map[string]any{
			"window_minutes": BatchWindowMinutes,
		})
	}
	batch := plan.Batches[index]
	if reasons := batch.gateReasons(); len(reasons) > 0 {
		return observationBatch{}, api.WithDetails(api.NewError(422, "BATCH_GATE_FAILED", "所选批次不满足时间一致性门禁，不能运行定位"), map[string]any{
			"batch_index":       batch.Index,
			"window_start":      batch.WindowStart,
			"window_end":        batch.WindowEnd,
			"observation_count": len(batch.Members),
			"station_count":     distinctStations(batch),
			"gate_reasons":      reasons,
		})
	}
	return batch, nil
}

func distinctStations(batch observationBatch) int {
	stations := make(map[uint]struct{}, len(batch.Members))
	for _, member := range batch.Members {
		stations[member.Station.ID] = struct{}{}
	}
	return len(stations)
}

// PreviewBatches 返回案例当前的分批方案、门禁原因与可用观测，供页面展示。
func (s *EstimateService) PreviewBatches(ctx context.Context, caseID uint) (dto.BatchPlanResponse, error) {
	caseRecord, err := s.caseRepo.Get(ctx, caseID)
	if err != nil {
		return dto.BatchPlanResponse{}, err
	}
	observations, err := s.observationRepo.ListForCase(ctx, caseID, true)
	if err != nil {
		return dto.BatchPlanResponse{}, err
	}
	return planToResponse(caseRecord.ID, buildBatchPlan(observations, caseRecord.FrequencyCenterHz)), nil
}

func planToResponse(caseID uint, plan batchPlan) dto.BatchPlanResponse {
	response := dto.BatchPlanResponse{
		CaseID: caseID, WindowMinutes: BatchWindowMinutes,
		Batches:              make([]dto.BatchView, 0, len(plan.Batches)),
		ExcludedObservations: make([]dto.BatchObservationView, 0, len(plan.Excluded)),
	}
	var earliest *int
	for _, batch := range plan.Batches {
		view := dto.BatchView{
			Index: batch.Index, WindowStart: batch.WindowStart, WindowEnd: batch.WindowEnd,
			ObservationIDs: make([]uint, 0, len(batch.Members)), StationIDs: make([]uint, 0),
			StationCodes: make([]string, 0), Observations: make([]dto.BatchObservationView, 0, len(batch.Members)),
		}
		stationSeen := make(map[uint]struct{}, len(batch.Members))
		for _, member := range batch.Members {
			view.ObservationIDs = append(view.ObservationIDs, member.Observation.ID)
			if _, ok := stationSeen[member.Station.ID]; !ok {
				stationSeen[member.Station.ID] = struct{}{}
				view.StationIDs = append(view.StationIDs, member.Station.ID)
				view.StationCodes = append(view.StationCodes, member.Station.StationCode)
			}
			batchIndex := batch.Index
			view.Observations = append(view.Observations, dto.BatchObservationView{
				ObservationID: member.Observation.ID, StationID: member.Station.ID,
				StationCode: member.Station.StationCode, ObservedAt: member.Observation.ObservedAt,
				Quality: string(member.Observation.Quality), BatchIndex: &batchIndex,
				Eligible: true, Reasons: []string{},
			})
		}
		view.ObservationCount = len(batch.Members)
		view.StationCount = len(stationSeen)
		view.GateReasons = batch.gateReasons()
		view.Runnable = len(view.GateReasons) == 0
		if view.Runnable && earliest == nil {
			value := batch.Index
			earliest = &value
		}
		response.Batches = append(response.Batches, view)
	}
	for _, observation := range plan.Excluded {
		stationID := uint(0)
		stationCode := ""
		if observation.Station != nil {
			stationID = observation.Station.ID
			stationCode = observation.Station.StationCode
		}
		reasons := plan.Reasons[observation.ID]
		if reasons == nil {
			reasons = []string{}
		}
		response.ExcludedObservations = append(response.ExcludedObservations, dto.BatchObservationView{
			ObservationID: observation.ID, StationID: stationID, StationCode: stationCode,
			ObservedAt: observation.ObservedAt, Quality: string(observation.Quality),
			BatchIndex: nil, Eligible: false, Reasons: reasons,
		})
	}
	response.EarliestRunnableIndex = earliest
	return response
}

func buildEstimate(caseID, userID uint, status string, result localization.Result, inputs []localization.Input, signature string, batch observationBatch, batchIDs []uint) (model.LocalizationEstimate, error) {
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
	batchJSON, err := json.Marshal(batchIDs)
	if err != nil {
		return model.LocalizationEstimate{}, fmt.Errorf("marshal batch observation IDs: %w", err)
	}
	if math.IsNaN(result.Point.Latitude) || math.IsNaN(result.Point.Longitude) {
		return model.LocalizationEstimate{}, api.NewError(422, "INVALID_ESTIMATE", "定位算法产生了无效坐标")
	}
	estimate := model.LocalizationEstimate{
		CaseID: caseID, AlgorithmVersion: localization.AlgorithmVersion,
		Latitude: result.Point.Latitude, Longitude: result.Point.Longitude,
		UncertaintyRadiusM: result.UncertaintyRadiusM, ResidualDeg: result.ResidualDeg,
		ConditionNumber: result.ConditionNumber, GeometryDegenerate: false,
		UsedObservationIDsJSON: datatypes.JSON(usedJSON), OutlierIDsJSON: datatypes.JSON(outlierJSON),
		ResidualsJSON: datatypes.JSON(residualJSON), InputSnapshotJSON: datatypes.JSON(snapshotJSON),
		BatchIndex: &batch.Index, BatchWindowStart: &batch.WindowStart, BatchWindowEnd: &batch.WindowEnd,
		BatchObservationIDsJSON: datatypes.JSON(batchJSON),
		EstimateStatus:          status, CreatedBy: userID,
	}
	if signature != "" {
		estimate.RunSignature = &signature
	}
	return estimate, nil
}
