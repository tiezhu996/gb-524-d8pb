package model

import (
	"time"

	"gorm.io/datatypes"
)

type LocalizationEstimate struct {
	ID                     uint           `json:"id" gorm:"primaryKey"`
	CaseID                 uint           `json:"case_id" gorm:"not null;index;uniqueIndex:idx_estimate_case_evidence,priority:1"`
	ParentEstimateID       *uint          `json:"parent_estimate_id" gorm:"index"`
	BatchIndex             int            `json:"batch_index" gorm:"not null;default:0;index"`
	BatchStart             *time.Time     `json:"batch_start"`
	BatchEnd               *time.Time     `json:"batch_end"`
	AlgorithmVersion       string         `json:"algorithm_version" gorm:"size:32;not null"`
	Latitude               float64        `json:"latitude" gorm:"not null"`
	Longitude              float64        `json:"longitude" gorm:"not null"`
	UncertaintyRadiusM     float64        `json:"uncertainty_radius_m" gorm:"not null"`
	ResidualDeg            float64        `json:"residual_deg" gorm:"not null"`
	ConditionNumber        float64        `json:"condition_number" gorm:"not null"`
	ConditionLimit         float64        `json:"condition_limit" gorm:"not null;default:0"`
	GeometryDegenerate     bool           `json:"geometry_degenerate" gorm:"not null"`
	OutlierRequested       bool           `json:"outlier_requested" gorm:"not null;default:false"`
	UsedObservationIDsJSON datatypes.JSON `json:"used_observation_ids_json" gorm:"type:jsonb;not null"`
	OutlierIDsJSON         datatypes.JSON `json:"outlier_ids_json" gorm:"type:jsonb;not null"`
	ResidualsJSON          datatypes.JSON `json:"residuals_json" gorm:"type:jsonb;not null"`
	InputSnapshotJSON      datatypes.JSON `json:"input_snapshot_json" gorm:"type:jsonb;not null"`
	BatchSnapshotJSON      datatypes.JSON `json:"batch_snapshot_json" gorm:"type:jsonb"`
	EvidenceHash           *string        `json:"evidence_hash" gorm:"size:64;uniqueIndex:idx_estimate_case_evidence,priority:2"`
	EstimateStatus         string         `json:"estimate_status" gorm:"size:24;not null;check:estimate_status IN ('complete','degenerate','outlier_candidate')"`
	CreatedBy              uint           `json:"created_by" gorm:"not null"`
	CreatedAt              time.Time      `json:"created_at"`
}

func (LocalizationEstimate) TableName() string { return "localization_estimates" }
