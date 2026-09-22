import { apiClient } from './client'
import type { LocalizationBatchPlan, LocalizationEstimate, LocalizationRunResult } from '../types/localization'

export const localizationApi = {
  list: (caseId?: number) => apiClient.get<LocalizationEstimate[]>(`/localizations${caseId ? `?case_id=${caseId}` : ''}`),
  get: (id: number) => apiClient.get<LocalizationEstimate>(`/localizations/${id}`),
  batches: (caseId: number) => apiClient.get<LocalizationBatchPlan>(`/cases/${caseId}/localization-batches`),
  run: (caseId: number, allowOutlier: boolean, batchIndex?: number) => apiClient.post<LocalizationRunResult>('/localizations/run', {
    case_id: caseId,
    batch_index: batchIndex ?? 0,
    allow_outlier: allowOutlier
  })
}
