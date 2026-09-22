package dto

import "time"

type RunLocalizationRequest struct {
	CaseID       uint `json:"case_id" binding:"required"`
	AllowOutlier bool `json:"allow_outlier"`
	// BatchIndex 选择参与估计的 30 分钟采集窗口批次（0 起，按时间排序）。
	// 省略时默认使用最早一个满足门禁的批次。
	BatchIndex *int `json:"batch_index"`
}

type ResidualEvidence struct {
	ObservationID uint    `json:"observation_id"`
	StationCode   string  `json:"station_code"`
	ObservedDeg   float64 `json:"observed_deg"`
	PredictedDeg  float64 `json:"predicted_deg"`
	ResidualDeg   float64 `json:"residual_deg"`
	Standardized  float64 `json:"standardized"`
}

type LocalizationRunResponse struct {
	Primary        any  `json:"primary"`
	Candidate      any  `json:"candidate,omitempty"`
	OutlierRemoved bool `json:"outlier_removed"`
}

// BatchObservationView 描述一条观测在当前分批方案中的状态。
type BatchObservationView struct {
	ObservationID uint      `json:"observation_id"`
	StationID     uint      `json:"station_id"`
	StationCode   string    `json:"station_code"`
	ObservedAt    time.Time `json:"observed_at"`
	Quality       string    `json:"quality"`
	BatchIndex    *int      `json:"batch_index"`
	// Eligible 表示该观测属于有效采集证据（未排除、站点启用、频率匹配）。
	Eligible bool     `json:"eligible"`
	Reasons  []string `json:"reasons"`
}

// BatchView 描述一个 30 分钟采集窗口批次及其门禁状态。
type BatchView struct {
	Index            int                    `json:"index"`
	WindowStart      time.Time              `json:"window_start"`
	WindowEnd        time.Time              `json:"window_end"`
	ObservationIDs   []uint                 `json:"observation_ids"`
	StationIDs       []uint                 `json:"station_ids"`
	StationCodes     []string               `json:"station_codes"`
	ObservationCount int                    `json:"observation_count"`
	StationCount     int                    `json:"station_count"`
	Runnable         bool                   `json:"runnable"`
	GateReasons      []string               `json:"gate_reasons"`
	Observations     []BatchObservationView `json:"observations"`
}

// BatchPlanResponse 是案例当前的批次一致性门禁快照。
type BatchPlanResponse struct {
	CaseID               uint                   `json:"case_id"`
	WindowMinutes        int                    `json:"window_minutes"`
	Batches              []BatchView            `json:"batches"`
	ExcludedObservations []BatchObservationView `json:"excluded_observations"`
	// EarliestRunnableIndex 最早一个满足门禁的批次，没有时为 nil。
	EarliestRunnableIndex *int `json:"earliest_runnable_index"`
}
