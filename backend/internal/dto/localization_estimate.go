package dto

import "time"

type RunLocalizationRequest struct {
	CaseID       uint `json:"case_id" binding:"required"`
	BatchIndex   int  `json:"batch_index"`
	AllowOutlier bool `json:"allow_outlier"`
}

type ResidualEvidence struct {
	ObservationID uint    `json:"observation_id"`
	StationCode   string  `json:"station_code"`
	ObservedDeg   float64 `json:"observed_deg"`
	PredictedDeg  float64 `json:"predicted_deg"`
	ResidualDeg   float64 `json:"residual_deg"`
	Standardized  float64 `json:"standardized"`
}

type RescheduleObservationRequest struct {
	ObservedAt time.Time `json:"observed_at" binding:"required"`
}

type LocalizationBatchObservation struct {
	ObservationID uint      `json:"observation_id"`
	StationID     uint      `json:"station_id"`
	StationCode   string    `json:"station_code"`
	ObservedAt    time.Time `json:"observed_at"`
	Quality       string    `json:"quality"`
	Available     bool      `json:"available"`
	Reasons       []string  `json:"reasons"`
}

type LocalizationBatch struct {
	BatchIndex   int                            `json:"batch_index"`
	WindowStart  time.Time                      `json:"window_start"`
	WindowEnd    time.Time                      `json:"window_end"`
	Eligible     bool                           `json:"eligible"`
	Reasons      []string                       `json:"reasons"`
	Observations []LocalizationBatchObservation `json:"observations"`
}

type LocalizationBatchPlan struct {
	CaseID                uint                           `json:"case_id"`
	WindowMinutes         int                            `json:"window_minutes"`
	RequiredObservations  int                            `json:"required_observations"`
	RequiredStationCount  int                            `json:"required_station_count"`
	Batches               []LocalizationBatch            `json:"batches"`
	UnbatchedObservations []LocalizationBatchObservation `json:"unbatched_observations"`
}

type LocalizationRunResponse struct {
	Primary        any                `json:"primary"`
	Candidate      any                `json:"candidate,omitempty"`
	OutlierRemoved bool               `json:"outlier_removed"`
	Reused         bool               `json:"reused"`
	Batch          *LocalizationBatch `json:"batch,omitempty"`
}
