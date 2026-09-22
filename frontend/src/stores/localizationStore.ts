import { create } from 'zustand'
import { localizationApi } from '../api/localizations'
import type { BatchPlan, LocalizationEstimate, LocalizationRunResult } from '../types/localization'

interface LocalizationState {
  estimates: LocalizationEstimate[]
  selected: LocalizationEstimate | null
  batchPlan: BatchPlan | null
  busy: boolean
  load: (caseId?: number) => Promise<void>
  loadBatches: (caseId: number) => Promise<BatchPlan>
  run: (caseId: number, allowOutlier: boolean, batchIndex?: number) => Promise<LocalizationRunResult>
  select: (estimate: LocalizationEstimate | null) => void
}

export const useLocalizationStore = create<LocalizationState>((set, get) => ({
  estimates: [],
  selected: null,
  batchPlan: null,
  busy: false,
  load: async (caseId) => {
    const response = await localizationApi.list(caseId)
    set({ estimates: response.data, selected: response.data[0] ?? null })
  },
  loadBatches: async (caseId) => {
    const response = await localizationApi.batches(caseId)
    set({ batchPlan: response.data })
    return response.data
  },
  run: async (caseId, allowOutlier, batchIndex) => {
    set({ busy: true })
    try {
      const response = await localizationApi.run(caseId, allowOutlier, batchIndex)
      if (response.data.reused) {
        set({ selected: response.data.primary })
      } else {
        const additions = response.data.candidate ? [response.data.candidate, response.data.primary] : [response.data.primary]
        set({ estimates: [...additions, ...get().estimates], selected: response.data.candidate ?? response.data.primary })
      }
      return response.data
    } finally {
      set({ busy: false })
    }
  },
  select: (estimate) => set({ selected: estimate })
}))
