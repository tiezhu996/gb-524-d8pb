import CheckCircleRounded from '@mui/icons-material/CheckCircleRounded'
import PendingRounded from '@mui/icons-material/PendingRounded'
import ReportProblemRounded from '@mui/icons-material/ReportProblemRounded'
import { Alert, Box, Chip, Stack, Table, TableBody, TableCell, TableHead, TableRow, Typography } from '@mui/material'
import type { BatchView } from '../../types/localization'
import { formatDateTime, gateReasonLabel, observationReasonLabel } from '../../utils/format'
import { QualityBadge } from './QualityBadge'

interface BatchGatePanelProps {
  batches: BatchView[]
  windowMinutes: number
  selectedIndex: number | null
  onSelect: (index: number) => void
}

// 批次时间一致性门禁：展示 30 分钟采集窗口、不满足原因和可用观测。
export function BatchGatePanel({ batches, windowMinutes, selectedIndex, onSelect }: BatchGatePanelProps) {
  if (batches.length === 0) {
    return <Alert severity="warning" icon={<ReportProblemRounded />}>案例当前没有可分批的有效观测，请先录入或恢复观测。</Alert>
  }
  return (
    <Stack gap={2}>
      <Typography variant="body2" color="text.secondary">
        同一案例按最早有效观测起的 {windowMinutes} 分钟采集窗口分批；只有同一批次内来自至少两个不同测向站的三条有效观测才能运行定位。跨批次观测保留在案例中，但不参与本次估计。
      </Typography>
      <Stack gap={1.5}>
        {batches.map((batch) => {
          const selected = selectedIndex === batch.index
          return (
            <Box
              key={batch.index}
              component="article"
              className={`batch-card ${batch.runnable ? 'is-runnable' : 'is-blocked'} ${selected ? 'is-selected' : ''}`}
              role="button"
              tabIndex={0}
              aria-pressed={selected}
              aria-label={`批次 ${batch.index + 1}，${batch.runnable ? '可运行定位' : '不满足门禁'}`}
              onClick={() => onSelect(batch.index)}
              onKeyDown={(event) => {
                if (event.key === 'Enter' || event.key === ' ') {
                  event.preventDefault()
                  onSelect(batch.index)
                }
              }}
            >
              <Stack direction={{ xs: 'column', sm: 'row' }} justifyContent="space-between" alignItems={{ xs: 'flex-start', sm: 'center' }} gap={1}>
                <Stack direction="row" spacing={1} alignItems="center" flexWrap="wrap" useFlexGap>
                  <strong>批次 {batch.index + 1}</strong>
                  <Chip size="small" label={`${formatDateTime(batch.window_start)} – ${formatDateTime(batch.window_end)}`} variant="outlined" />
                  <Chip size="small" label={`${batch.observation_count} 条观测`} />
                  <Chip size="small" label={`${batch.station_count} 个测向站`} />
                </Stack>
                {batch.runnable
                  ? <Chip size="small" color="success" icon={<CheckCircleRounded />} label="满足门禁，可运行" />
                  : <Chip size="small" color="warning" icon={<PendingRounded />} label="不满足门禁" />}
              </Stack>
              {!batch.runnable && (
                <Stack direction="row" spacing={1} mt={1} flexWrap="wrap" useFlexGap>
                  {batch.gate_reasons.map((reason) => (
                    <Chip key={reason} size="small" color="warning" variant="outlined" icon={<ReportProblemRounded />} label={gateReasonLabel(reason)} />
                  ))}
                </Stack>
              )}
              <Box className="table-scroll batch-observation-scroll" mt={1}>
                <Table size="small" aria-label={`批次 ${batch.index + 1} 可用观测`}>
                  <TableHead>
                    <TableRow>
                      <TableCell>观测</TableCell>
                      <TableCell>测向站</TableCell>
                      <TableCell>采集时间</TableCell>
                      <TableCell>质量</TableCell>
                    </TableRow>
                  </TableHead>
                  <TableBody>
                    {batch.observations.map((observation) => (
                      <TableRow key={observation.observation_id}>
                        <TableCell><strong>#{observation.observation_id}</strong></TableCell>
                        <TableCell>{observation.station_code}</TableCell>
                        <TableCell className="secondary-text">{formatDateTime(observation.observed_at)}</TableCell>
                        <TableCell><QualityBadge quality={observation.quality as 'good' | 'fair' | 'poor' | 'excluded'} /></TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </Box>
            </Box>
          )
        })}
      </Stack>
    </Stack>
  )
}

interface ExcludedBatchObservationsProps {
  observations: BatchView['observations']
}

export function ExcludedBatchObservations({ observations }: ExcludedBatchObservationsProps) {
  if (observations.length === 0) return null
  return (
    <Box className="data-section">
      <Typography component="h3" variant="subtitle1" gutterBottom>保留在案例中但不参与本次估计的观测</Typography>
      <Box className="table-scroll">
        <Table size="small" aria-label="不参与定位的观测">
          <TableHead>
            <TableRow>
              <TableCell>观测</TableCell>
              <TableCell>测向站</TableCell>
              <TableCell>采集时间</TableCell>
              <TableCell>质量</TableCell>
              <TableCell>不参与原因</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {observations.map((observation) => (
              <TableRow key={observation.observation_id} className="row-muted">
                <TableCell><strong>#{observation.observation_id}</strong></TableCell>
                <TableCell>{observation.station_code || `站点 #${observation.station_id}`}</TableCell>
                <TableCell className="secondary-text">{formatDateTime(observation.observed_at)}</TableCell>
                <TableCell><QualityBadge quality={observation.quality as 'good' | 'fair' | 'poor' | 'excluded'} /></TableCell>
                <TableCell>{observation.reasons.map(observationReasonLabel).join('；')}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Box>
    </Box>
  )
}
