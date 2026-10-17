// 按模型名推断厂商，用于路由页的快速筛选。
export interface VendorOption {
  name: string
  count: number
}

const RULES: Array<[RegExp, string]> = [
  [/^(openai\/)/, 'OpenAI'],
  [/^(gpt-|chatgpt|o[1-9](\b|-))/i, 'OpenAI'],
  [/^(deepseek\/)/, 'DeepSeek'],
  [/^(deepseek)/i, 'DeepSeek'],
  [/^(anthropic\/)/, 'Anthropic'],
  [/(claude)/i, 'Anthropic'],
  [/^(google\/)/, 'Google'],
  [/(gemini|gemma)/i, 'Google'],
  [/^(qwen\/|alibaba\/)/, 'Qwen'],
  [/(qwen)/i, 'Qwen'],
  [/^(z-ai\/|zhipu\/)/, 'GLM'],
  [/(glm)/i, 'GLM'],
  [/^(moonshot\/)/, 'Kimi'],
  [/(kimi)/i, 'Kimi'],
  [/(minimax)/i, 'MiniMax'],
  [/^(x-ai\/)/, 'xAI'],
  [/(grok)/i, 'xAI'],
  [/(mistral)/i, 'Mistral'],
]

export function vendorOf(name: string): string {
  const m = (name || '').trim()
  if (!m) return 'Other'
  for (const [re, vendor] of RULES) {
    if (re.test(m)) return vendor
  }
  const head = m.split('/')[0]
  return head ? head.charAt(0).toUpperCase() + head.slice(1) : 'Other'
}

export function vendorOptions(names: string[]): VendorOption[] {
  const m = new Map<string, number>()
  for (const n of names) {
    const v = vendorOf(n)
    m.set(v, (m.get(v) || 0) + 1)
  }
  return [...m.entries()]
    .map(([name, count]) => ({ name, count }))
    .sort((a, b) => b.count - a.count || a.name.localeCompare(b.name))
}
