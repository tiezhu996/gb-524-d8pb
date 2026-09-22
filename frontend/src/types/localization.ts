import type { ObservationQuality } from './observation'

export interface ResidualEvidence {
  observation_id: number
  station_code: string
  observed_deg: number
  predicted_deg: number
  residual_deg: number
  standardized: number
}

export interface LocalizationInputSnapshot {
  observation_id: number
  station_code: string
  latitude: number
  longitude: number
  bearing_deg: number
  accuracy_deg: number
  quality_weight: number
}

export interface LocalizationEstimate {
  id: number
  case_id: number
  parent_estimate_id: number | null
  batch_index: number
  batch_start: string | null
  batch_end: string | null
  algorithm_version: string
  latitude: number
  longitude: number
  uncertainty_radius_m: number
  residual_deg: number
  condition_number: number
  condition_limit: number
  geometry_degenerate: boolean
  outlier_requested: boolean
  used_observation_ids_json: number[]
  outlier_ids_json: number[]
  residuals_json: ResidualEvidence[]
  input_snapshot_json: LocalizationInputSnapshot[]
  batch_snapshot_json: LocalizationBatch | null
  evidence_hash: string | null
  estimate_status: 'complete' | 'degenerate' | 'outlier_candidate'
  created_by: number
  created_at: string
}

export interface LocalizationBatchObservation {
  observation_id: number
  station_id: number
  station_code: string
  observed_at: string
  quality: ObservationQuality
  available: boolean
  reasons: string[]
}

export interface LocalizationBatch {
  batch_index: number
  window_start: string
  window_end: string
  eligible: boolean
  reasons: string[]
  observations: LocalizationBatchObservation[]
}

export interface LocalizationBatchPlan {
  case_id: number
  window_minutes: number
  required_observations: number
  required_station_count: number
  batches: LocalizationBatch[]
  unbatched_observations: LocalizationBatchObservation[]
}

export interface LocalizationRunResult {
  primary: LocalizationEstimate
  candidate?: LocalizationEstimate
  reused?: boolean
  batch?: LocalizationBatch
}

