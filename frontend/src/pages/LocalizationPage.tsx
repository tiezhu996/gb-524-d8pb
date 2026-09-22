import { useEffect, useMemo, useState } from 'react'
import PlayArrowRounded from '@mui/icons-material/PlayArrowRounded'
import ScienceRounded from '@mui/icons-material/ScienceRounded'
import { Alert, Box, Button, Chip, FormControlLabel, MenuItem, Stack, Switch, Table, TableBody, TableCell, TableHead, TableRow, TextField, Typography } from '@mui/material'
import { BearingPlot } from '../components/common/BearingPlot'
import { PageHeader } from '../components/common/PageHeader'
import { QualityBadge } from '../components/common/QualityBadge'
import { useAuth } from '../hooks/useAuth'
import { useLocalizationRun } from '../hooks/useLocalizationRun'
import { useCaseStore } from '../stores/caseStore'
import { useLocalizationStore } from '../stores/localizationStore'
import { useObservationStore } from '../stores/observationStore'
import { useStationStore } from '../stores/stationStore'
import type { LocalizationBatch, LocalizationBatchPlan, LocalizationEstimate } from '../types/localization'
import { formatCoordinate, formatDateTime, formatDecimal, formatFrequency } from '../utils/format'

export function LocalizationPage() {
  const { hasRole } = useAuth()
  const cases = useCaseStore((state) => state.cases)
  const loadCases = useCaseStore((state) => state.load)
  const stations = useStationStore((state) => state.stations)
  const loadStations = useStationStore((state) => state.load)
  const observations = useObservationStore((state) => state.observations)
  const loadObservations = useObservationStore((state) => state.load)
  const estimates = useLocalizationStore((state) => state.estimates)
  const selected = useLocalizationStore((state) => state.selected)
  const select = useLocalizationStore((state) => state.select)
  const loadEstimates = useLocalizationStore((state) => state.load)
  const loadBatches = useLocalizationStore((state) => state.loadBatches)
  const batchPlan = useLocalizationStore((state) => state.batchPlan)
  const { execute, busy, lastResult } = useLocalizationRun()
  const [caseId, setCaseId] = useState(0)
  const [allowOutlier, setAllowOutlier] = useState(true)
  const [selectedBatchIndex, setSelectedBatchIndex] = useState(0)

  useEffect(() => {
    void Promise.all([loadCases(), loadStations()])
  }, [loadCases, loadStations])

  useEffect(() => {
    if (!caseId) {
      const analyzing = cases.find((item) => item.case_status === 'analyzing')
      if (analyzing) setCaseId(analyzing.id)
    }
  }, [caseId, cases])

  useEffect(() => {
    if (!caseId) return
    setSelectedBatchIndex(0)
    void Promise.all([loadObservations(caseId), loadEstimates(caseId), loadBatches(caseId)])
  }, [caseId, loadBatches, loadEstimates, loadObservations])

  const selectedCase = cases.find((item) => item.id === caseId)
  const residuals = selected?.residuals_json ?? []
  const observationById = useMemo(() => new Map(observations.map((item) => [item.id, item])), [observations])
  const defaultEligibleBatch = batchPlan?.batches.find((batch) => batch.eligible)?.batch_index ?? 0
  const runnableBatch = selectedBatchIndex || defaultEligibleBatch
  const selectedBatch = batchPlan?.batches.find((batch) => batch.batch_index === runnableBatch)
  const plotObservationIds = useMemo(() => new Set((selectedBatch?.observations ?? []).map((item) => item.observation_id)), [selectedBatch])
  const batchObservations = useMemo(() => observations.filter((item) => plotObservationIds.has(item.id)), [observations, plotObservationIds])
  const canRun = Boolean(caseId && selectedCase?.case_status === 'analyzing' && selectedBatch?.eligible)

  const run = async () => {
    if (!caseId || !runnableBatch) return
    await execute(caseId, allowOutlier, runnableBatch)
    await Promise.all([loadCases(), loadBatches(caseId), loadEstimates(caseId)])
  }

  return (
    <>
      <PageHeader
        eyebrow="WEIGHTED BEARING INTERSECTION / WLS V1"
        title="三角定位证据台"
        summary={selectedCase ? `${selectedCase.case_code} · ${formatFrequency(selectedCase.frequency_center_hz)} · ${selectedCase.active_observation_count} 条有效观测` : '选择 analyzing 案例后运行离线定位'}
        actions={hasRole('analyst', 'admin') ? <Button variant="contained" startIcon={<PlayArrowRounded />} disabled={!canRun || busy} onClick={() => void run()}>{busy ? '正在计算' : '运行批次定位'}</Button> : undefined}
      />

      <section className="control-strip localization-controls">
        <TextField select size="small" label="分析案例" value={caseId || ''} onChange={(event) => setCaseId(Number(event.target.value))} sx={{ minWidth: 330 }}>
          {cases.filter((item) => item.case_status === 'analyzing' || item.id === caseId).map((item) => <MenuItem key={item.id} value={item.id}>{item.case_code} · {item.title}</MenuItem>)}
        </TextField>
        <FormControlLabel control={<Switch checked={allowOutlier} onChange={(event) => setAllowOutlier(event.target.checked)} />} label="生成可解释离群候选" />
        {lastResult && <Alert severity={lastResult.reused ? 'info' : lastResult.candidate ? 'warning' : 'success'}>{lastResult.reused ? '相同批次与证据已运行过，返回既有结果，未重复保存。' : lastResult.candidate ? '已保留原估计，并生成剔除观测的候选重算。' : '定位批次与证据快照已保存。'}</Alert>}
      </section>

      <Alert severity="info" icon={<ScienceRounded />} className="safety-alert">只有同一 30 分钟采集批次内、来自至少两个测向站的三条有效观测参与本次估计；跨批次和不可用观测保留在案例证据中。</Alert>

      {batchPlan && <BatchGate plan={batchPlan} selectedBatchIndex={selectedBatchIndex} onSelectBatch={setSelectedBatchIndex} />}

      <section className="localization-grid">
        <div className="plot-section plot-primary">
          <BearingPlot stations={stations} observations={selected ? observations : batchObservations} estimate={selected} height={520} />
        </div>
        <aside className="estimate-rail" aria-label="定位结果历史">
          <Typography component="h2" variant="h6">不可覆盖的运行历史</Typography>
          <Stack gap={1.5} mt={2}>
            {estimates.map((estimate) => <EstimateButton key={estimate.id} estimate={estimate} selected={selected?.id === estimate.id} onClick={() => select(estimate)} />)}
            {estimates.length === 0 && <Typography color="text.secondary">尚无运行结果。满足时间一致性门禁后可运行定位。</Typography>}
          </Stack>
        </aside>
      </section>

      <section className="data-section" aria-labelledby="residual-title">
        <Stack direction={{ xs: 'column', md: 'row' }} justifyContent="space-between" gap={1} mb={2}>
          <Typography id="residual-title" component="h2" variant="h6">逐站角度残差</Typography>
          {selected && <Typography variant="body2" color="text.secondary">批次 #{selected.batch_index} · {formatDateTime(selected.batch_start)} 至 {formatDateTime(selected.batch_end)} · 算法 {selected.algorithm_version}</Typography>}
        </Stack>
        <Box className="table-scroll">
          <Table size="small" aria-label="定位残差证据">
            <TableHead><TableRow><TableCell>观测 / 测向站</TableCell><TableCell>观测方位</TableCell><TableCell>预测方位</TableCell><TableCell>角度残差</TableCell><TableCell>标准化残差</TableCell><TableCell>质量</TableCell></TableRow></TableHead>
            <TableBody>
              {residuals.map((residual) => {
                const observation = observationById.get(residual.observation_id)
                return <TableRow key={residual.observation_id} className={selected?.outlier_ids_json.includes(residual.observation_id) ? 'row-warning' : ''}>
                  <TableCell><strong>#{residual.observation_id}</strong> · {residual.station_code}</TableCell>
                  <TableCell className="numeric">{formatDecimal(residual.observed_deg, 2)}°</TableCell>
                  <TableCell className="numeric">{formatDecimal(residual.predicted_deg, 2)}°</TableCell>
                  <TableCell className="numeric">{residual.residual_deg >= 0 ? '+' : ''}{formatDecimal(residual.residual_deg, 2)}°</TableCell>
                  <TableCell className="numeric">{formatDecimal(residual.standardized, 2)} σ {selected?.outlier_ids_json.includes(residual.observation_id) && <strong> · 离群证据</strong>}</TableCell>
                  <TableCell>{observation ? <QualityBadge quality={observation.quality} /> : '历史快照'}</TableCell>
                </TableRow>
              })}
              {!selected && <TableRow><TableCell colSpan={6}>选择或运行一条定位结果后显示逐站残差。</TableCell></TableRow>}
            </TableBody>
          </Table>
        </Box>
      </section>
    </>
  )
}

function BatchGate({ plan, selectedBatchIndex, onSelectBatch }: { plan: LocalizationBatchPlan; selectedBatchIndex: number; onSelectBatch: (index: number) => void }) {
  const shownBatch = selectedBatchIndex || plan.batches.find((batch) => batch.eligible)?.batch_index || plan.batches[0]?.batch_index || 0
  const current = plan.batches.find((batch) => batch.batch_index === shownBatch)
  const currentIds = new Set(current?.observations.map((item) => item.observation_id) ?? [])
  const crossBatch = plan.batches.filter((batch) => batch.batch_index !== shownBatch).flatMap((batch) => batch.observations.map((item) => ({ ...item, reasons: [`位于批次 #${batch.batch_index}`] })))
  const retained = [...plan.unbatched_observations, ...crossBatch.filter((item) => !currentIds.has(item.observation_id))]
  return (
    <section className="data-section batch-gate" aria-labelledby="batch-title">
      <Stack direction={{ xs: 'column', md: 'row' }} justifyContent="space-between" gap={1} mb={2}>
        <div>
          <Typography id="batch-title" component="h2" variant="h6">定位批次时间一致性门禁</Typography>
          <Typography variant="body2" color="text.secondary">按最早有效观测起每 {plan.window_minutes} 分钟分批；要求同一批次 ≥ {plan.required_observations} 条观测且覆盖 ≥ {plan.required_station_count} 个测向站。</Typography>
        </div>
        <Stack direction="row" gap={1} flexWrap="wrap">
          {plan.batches.map((batch) => <Chip key={batch.batch_index} clickable color={batch.eligible ? 'success' : 'default'} variant={current?.batch_index === batch.batch_index ? 'filled' : 'outlined'} label={`批次 #${batch.batch_index} · ${batch.observations.length} 条`} onClick={() => onSelectBatch(batch.batch_index)} />)}
        </Stack>
      </Stack>
      {current && <BatchDetail batch={current} />}
      <Box mt={2}>
        <Typography variant="subtitle2">保留但不参与本次估计的观测</Typography>
        <Table size="small">
          <TableHead><TableRow><TableCell>观测</TableCell><TableCell>原因</TableCell></TableRow></TableHead>
          <TableBody>
            {retained.map((item) => <TableRow key={`${item.station_id}-${item.observation_id}`}><TableCell>#{item.observation_id} · {item.station_code}</TableCell><TableCell>{item.reasons.join('；')}</TableCell></TableRow>)}
            {retained.length === 0 && <TableRow><TableCell colSpan={2}>当前批次以外没有保留但不参与本次估计的观测。</TableCell></TableRow>}
          </TableBody>
        </Table>
      </Box>
    </section>
  )
}

function BatchDetail({ batch }: { batch: LocalizationBatch }) {
  return (
    <Box className="batch-detail">
      <Stack direction={{ xs: 'column', md: 'row' }} justifyContent="space-between" gap={1} mb={1}>
        <Typography variant="subtitle1">批次 #{batch.batch_index}：{formatDateTime(batch.window_start)} — {formatDateTime(batch.window_end)}</Typography>
        <Chip size="small" color={batch.eligible ? 'success' : 'warning'} label={batch.eligible ? '满足运行条件' : '不可运行'} />
      </Stack>
      {!batch.eligible && <Alert severity="warning" sx={{ mb: 1 }}>{batch.reasons.join('；')}</Alert>}
      <Table size="small">
        <TableHead><TableRow><TableCell>观测</TableCell><TableCell>测向站</TableCell><TableCell>观测时间</TableCell><TableCell>质量</TableCell><TableCell>状态</TableCell></TableRow></TableHead>
        <TableBody>
          {batch.observations.map((item) => <TableRow key={item.observation_id}><TableCell>#{item.observation_id}</TableCell><TableCell>{item.station_code}</TableCell><TableCell>{formatDateTime(item.observed_at)}</TableCell><TableCell><QualityBadge quality={item.quality} /></TableCell><TableCell>{item.available ? '参与定位' : item.reasons.join('；')}</TableCell></TableRow>)}
          {batch.observations.length === 0 && <TableRow><TableCell colSpan={5}>该批次没有有效观测。</TableCell></TableRow>}
        </TableBody>
      </Table>
    </Box>
  )
}

function EstimateButton({ estimate, selected, onClick }: { estimate: LocalizationEstimate; selected: boolean; onClick: () => void }) {
  return (
    <button type="button" className={`estimate-item ${selected ? 'is-selected' : ''}`} onClick={onClick}>
      <span className="estimate-item-top"><strong>运行 #{estimate.id} · 批次 #{estimate.batch_index}</strong><span>{estimate.estimate_status === 'outlier_candidate' ? '△ 离群候选' : '◆ 原始估计'}</span></span>
      <span className="estimate-coordinate">{formatCoordinate(estimate.latitude)}, {formatCoordinate(estimate.longitude)}</span>
      <span className="estimate-metrics">残差 {formatDecimal(estimate.residual_deg)}° · 半径 {formatDecimal(estimate.uncertainty_radius_m, 0)} m</span>
    </button>
  )
}
