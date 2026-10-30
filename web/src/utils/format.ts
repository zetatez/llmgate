// 统一时间显示：本地时区，避免各页面混用 12 小时制 / 美式日期。
function pad(n: number): string {
  return String(n).padStart(2, '0')
}

/** YYYY-MM-DD HH:mm */
export function fmtDateTime(ts?: number | null, fallback = '—'): string {
  if (!ts) return fallback
  const d = new Date(ts * 1000)
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

/** MM-DD HH:mm（用于窄表格） */
export function fmtShort(ts?: number | null, fallback = '—'): string {
  if (!ts) return fallback
  const d = new Date(ts * 1000)
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

/** YYYY-MM-DD */
export function fmtDate(ts?: number | null, fallback = '—'): string {
  if (!ts) return fallback
  const d = new Date(ts * 1000)
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}
