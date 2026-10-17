import type { EChartsOption } from 'echarts'

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

/** 深色的 item tooltip 基样式。 */
export const ITEM_TOOLTIP = {
  trigger: 'item' as const,
  backgroundColor: 'rgba(18,22,32,.94)',
  borderWidth: 0,
  padding: [8, 12],
  textStyle: { color: '#e8eaf0', fontSize: 12 },
}

export const AXIS_LINE = { lineStyle: { color: '#2a2c33' } }
export const SPLIT_LINE = { lineStyle: { color: '#23252c' } }
export const AXIS_LABEL = { color: '#8b909c' }

/** 展开 ECharts option 的类型工具。 */
export type ChartOption = EChartsOption
