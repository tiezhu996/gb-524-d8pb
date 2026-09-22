package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"spectrum-interference-triangulation/backend/internal/constants"
	"spectrum-interference-triangulation/backend/internal/dto"
	"spectrum-interference-triangulation/backend/internal/model"
	"spectrum-interference-triangulation/backend/pkg/api"
)

var ErrDuplicateEstimateRun = errors.New("duplicate localization estimate run")

type EstimateRepository struct {
	db *gorm.DB
}

type EstimateRun struct {
	Primary   model.LocalizationEstimate
	Candidate *model.LocalizationEstimate
}

func NewEstimateRepository(db *gorm.DB) *EstimateRepository {
	return &EstimateRepository{db: db}
}

func (r *EstimateRepository) List(ctx context.Context, caseID uint) ([]model.LocalizationEstimate, error) {
	query := r.db.WithContext(ctx).Model(&model.LocalizationEstimate{})
	if caseID > 0 {
		query = query.Where("case_id = ?", caseID)
	}
	var estimates []model.LocalizationEstimate
	if err := query.Order("created_at DESC, id DESC").Find(&estimates).Error; err != nil {
		return nil, fmt.Errorf("list localization estimates: %w", err)
	}
	return estimates, nil
}

func (r *EstimateRepository) Get(ctx context.Context, id uint) (model.LocalizationEstimate, error) {
	var estimate model.LocalizationEstimate
	if err := r.db.WithContext(ctx).First(&estimate, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return model.LocalizationEstimate{}, api.NewError(404, "ESTIMATE_NOT_FOUND", "定位结果不存在")
		}
		return model.LocalizationEstimate{}, fmt.Errorf("get localization estimate: %w", err)
	}
	return estimate, nil
}

func (r *EstimateRepository) FindRun(ctx context.Context, caseID uint, evidenceHash string) (EstimateRun, bool, error) {
	var primary model.LocalizationEstimate
	err := r.db.WithContext(ctx).
		Where("case_id = ? AND evidence_hash = ? AND estimate_status = ?", caseID, evidenceHash, constants.EstimateComplete).
		First(&primary).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return EstimateRun{}, false, nil
	}
	if err != nil {
		return EstimateRun{}, false, fmt.Errorf("find localization run: %w", err)
	}
	run := EstimateRun{Primary: primary}
	var candidate model.LocalizationEstimate
	findErr := r.db.WithContext(ctx).
		Where("parent_estimate_id = ? AND estimate_status = ?", primary.ID, constants.EstimateCandidate).
		First(&candidate).Error
	if findErr == nil {
		run.Candidate = &candidate
	} else if !errors.Is(findErr, gorm.ErrRecordNotFound) {
		return EstimateRun{}, false, fmt.Errorf("find localization candidate: %w", findErr)
	}
	return run, true, nil
}

func (r *EstimateRepository) CreateRun(ctx context.Context, caseID, version uint, primary *model.LocalizationEstimate, candidate *model.LocalizationEstimate, allowOutlier bool, conditionLimit float64, batch dto.LocalizationBatch, actor Actor) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var caseRecord model.InterferenceCase
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&caseRecord, caseID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return api.NewError(404, "CASE_NOT_FOUND", "干扰案例不存在")
			}
			return fmt.Errorf("lock case for localization run: %w", err)
		}
		if caseRecord.Version != version || caseRecord.CaseStatus != constants.CaseAnalyzing {
			return api.NewError(409, "CASE_VERSION_CONFLICT", "案例状态或版本已变化，请刷新后重试")
		}
		if primary.EvidenceHash != nil {
			var count int64
			if err := tx.Model(&model.LocalizationEstimate{}).
				Where("case_id = ? AND evidence_hash = ? AND estimate_status = ?", caseID, *primary.EvidenceHash, constants.EstimateComplete).
				Count(&count).Error; err != nil {
				return fmt.Errorf("check duplicate localization evidence: %w", err)
			}
			if count > 0 {
				return ErrDuplicateEstimateRun
			}
		}
		if err := tx.Create(primary).Error; err != nil {
			if isDuplicateKeyError(err) {
				return ErrDuplicateEstimateRun
			}
			return fmt.Errorf("save primary estimate: %w", err)
		}
		if candidate != nil {
			candidate.ParentEstimateID = &primary.ID
			if err := tx.Create(candidate).Error; err != nil {
				if isDuplicateKeyError(err) {
					return ErrDuplicateEstimateRun
				}
				return fmt.Errorf("save outlier candidate estimate: %w", err)
			}
		}
		if err := tx.Model(&model.InterferenceCase{}).Where("id = ?", caseID).
			UpdateColumn("version", gorm.Expr("version + 1")).Error; err != nil {
			return fmt.Errorf("update case localization version: %w", err)
		}
		after := map[string]any{
			"primary_id":         primary.ID,
			"batch_index":        primary.BatchIndex,
			"batch_start":        primary.BatchStart,
			"batch_end":          primary.BatchEnd,
			"batch_observations": batch.Observations,
			"algorithm_version":  primary.AlgorithmVersion,
			"allow_outlier":      allowOutlier,
			"condition_limit":    conditionLimit,
			"evidence_hash":      primary.EvidenceHash,
			"candidate_id": func() uint {
				if candidate == nil {
					return 0
				}
				return candidate.ID
			}(),
			"residual_deg":         primary.ResidualDeg,
			"condition_number":     primary.ConditionNumber,
			"geometry_degenerate":  primary.GeometryDegenerate,
			"used_observation_ids": primary.UsedObservationIDsJSON,
			"outlier_ids":          primary.OutlierIDsJSON,
		}
		audit := NewAudit(actor, "localization_estimate.created", "interference_case", caseID, map[string]any{"version": version}, after)
		if err := tx.Create(&audit).Error; err != nil {
			return fmt.Errorf("audit localization run: %w", err)
		}
		return nil
	})
}

func isDuplicateKeyError(err error) bool {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "duplicate key") || strings.Contains(message, "unique constraint")
}
