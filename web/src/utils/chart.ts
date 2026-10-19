/** 图表统一配色（贴近 opencode data 的低饱和深色调）。 */
export const PALETTE = [
  '#8b93d6', '#8fae9b', '#d3b27f', '#c98b96', '#7a93b8',
  '#b5a7dd', '#a3b0c2', '#d9a05f', '#9aa7c9', '#c9c2dc',
]

/** 深色的 axis tooltip 基样式。 */
export const AXIS_TOOLTIP = {
  trigger: 'axis' as const,
  backgroundColor: 'rgba(18,22,32,.94)',
  borderWidth: 0,
  padding: [8, 12],
  textStyle: { color: '#e8eaf0', fontSize: 12 },
}

export interface DailyTokenPoint {
  date: string
  name: string
  tokens: number
}

/**
 * 把「日期 × 名称」的 token 点聚合成按日堆叠柱数据：
 * 取总量前 topN 的名称，其余合并为 Other。
 */
export function buildDailyStack(dates: string[], points: DailyTokenPoint[], topN = 7) {
  const totals = new Map<string, number>()
  for (const p of points) totals.set(p.name, (totals.get(p.name) || 0) + p.tokens)
  const top = [...totals.entries()]
    .filter(([, v]) => v > 0)
    .sort((a, b) => b[1] - a[1])
    .slice(0, topN)
    .map(([n]) => n)
  const nameIndex = new Map(top.map((n, i) => [n, i]))
  const dateIndex = new Map(dates.map((d, i) => [d, i]))
  const series = top.map(() => dates.map(() => 0))
  const other = dates.map(() => 0)
  for (const p of points) {
    const di = dateIndex.get(p.date)
    if (di === undefined) continue
    const ni = nameIndex.get(p.name)
    if (ni === undefined) other[di] += p.tokens
    else series[ni][di] += p.tokens
  }
  const names = [...top]
  const out = [...series]
  if (other.some((v) => v > 0)) {
    names.push('Other')
    out.push(other)
  }
  return { names, series: out }
}
