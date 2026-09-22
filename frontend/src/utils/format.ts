export function formatFrequency(value: number): string {
  if (value >= 1_000_000_000) return `${(value / 1_000_000_000).toFixed(6)} GHz`
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(6)} MHz`
  if (value >= 1_000) return `${(value / 1_000).toFixed(3)} kHz`
  return `${value.toFixed(0)} Hz`
}

export function formatDateTime(value?: string | null): string {
  if (!value) return '未记录'
  return new Intl.DateTimeFormat('zh-CN', {
    year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false
  }).format(new Date(value))
}

export function formatCoordinate(value: number): string {
  return value.toFixed(6)
}

export function formatDecimal(value: number, digits = 2): string {
  return new Intl.NumberFormat('zh-CN', {
    minimumFractionDigits: digits,
    maximumFractionDigits: digits
  }).format(value)
}

// 定位批次时间一致性门禁原因码的中文说明。
export function gateReasonLabel(code: string): string {
  switch (code) {
    case 'OBSERVATION_COUNT_BELOW_3':
      return '批次内有效观测不足 3 条'
    case 'STATION_COUNT_BELOW_2':
      return '观测来自少于 2 个不同测向站'
    default:
      return code
  }
}

// 观测被排除于有效采集证据之外的原因码中文说明。
export function observationReasonLabel(code: string): string {
  switch (code) {
    case 'OBSERVATION_QUALITY_EXCLUDED':
      return '观测已被人工排除'
    case 'STATION_NOT_ACTIVE':
      return '测向站未处于启用状态'
    case 'FREQUENCY_OUT_OF_BAND':
      return '观测频率超出案例中心频率带宽'
    default:
      return code
  }
}

