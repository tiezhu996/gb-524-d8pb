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
  algorithm_version: string
  latitude: number
  longitude: number
  uncertainty_radius_m: number
  residual_deg: number
  condition_number: number
  geometry_degenerate: boolean
  used_observation_ids_json: number[]
  outlier_ids_json: number[]
  residuals_json: ResidualEvidence[]
  input_snapshot_json: LocalizationInputSnapshot[]
  run_signature?: string | null
  batch_index?: number | null
  batch_window_start?: string | null
  batch_window_end?: string | null
  batch_observation_ids_json?: number[]
  estimate_status: 'complete' | 'degenerate' | 'outlier_candidate'
  created_by: number
  created_at: string
}

export interface LocalizationRunResult {
  primary: LocalizationEstimate
  candidate?: LocalizationEstimate
  batch_index: number
  batch_window_start: string
  batch_window_end: string
  reused: boolean
}

// 观测在当前分批方案中的状态。
export interface BatchObservationView {
  observation_id: number
  station_id: number
  station_code: string
  observed_at: string
  quality: string
  batch_index: number | null
  eligible: boolean
  reasons: string[]
}

export interface BatchView {
  index: number
  window_start: string
  window_end: string
  observation_ids: number[]
  station_ids: number[]
  station_codes: string[]
  observation_count: number
  station_count: number
  runnable: boolean
  gate_reasons: string[]
  observations: BatchObservationView[]
}

export interface BatchPlan {
  case_id: number
  window_minutes: number
  batches: BatchView[]
  excluded_observations: BatchObservationView[]
  earliest_runnable_index: number | null
}

