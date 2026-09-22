import { apiClient } from './client'
import type { BatchPlan, LocalizationEstimate, LocalizationRunResult } from '../types/localization'

export const localizationApi = {
  list: (caseId?: number) => apiClient.get<LocalizationEstimate[]>(`/localizations${caseId ? `?case_id=${caseId}` : ''}`),
  get: (id: number) => apiClient.get<LocalizationEstimate>(`/localizations/${id}`),
  batches: (caseId: number) => apiClient.get<BatchPlan>(`/cases/${caseId}/batches`),
  run: (caseId: number, allowOutlier: boolean, batchIndex?: number) => apiClient.post<LocalizationRunResult>('/localizations/run', {
    case_id: caseId,
    allow_outlier: allowOutlier,
    batch_index: batchIndex
  })
}

