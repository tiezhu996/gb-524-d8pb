import { apiClient } from './client'
import type { BearingObservation, BatchValidation, ObservationInput, RescheduleObservationInput } from '../types/observation'

export const observationApi = {
  list: (caseId?: number, stationId?: number) => {
    const params = new URLSearchParams({ page_size: '100' })
    if (caseId) params.set('case_id', String(caseId))
    if (stationId) params.set('station_id', String(stationId))
    return apiClient.getPage<BearingObservation[]>(`/observations?${params}`)
  },
  get: (id: number) => apiClient.get<BearingObservation>(`/observations/${id}`),
  create: (input: ObservationInput) => apiClient.post<BearingObservation>('/observations', input),
  exclude: (id: number, reason: string) => apiClient.post<BearingObservation>(`/observations/${id}/exclude`, { reason }),
  reschedule: (id: number, input: RescheduleObservationInput) => apiClient.put<BearingObservation>(`/observations/${id}/reschedule`, input),
  validateCase: (caseId: number) => apiClient.get<BatchValidation>(`/cases/${caseId}/validate-observations`)
}
