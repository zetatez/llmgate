// 按模型名推断厂商，用于路由页的快速筛选。
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

/**
 * 实际用量统计用的厂商归类：只把能识别的规则或带 provider 前缀的模型算作厂商，
 * 其余（如 level1 等内部/占位模型名）统一归入 Other，避免出现虚假厂商标签。
 */
export function usageVendorOf(name: string): string {
  const m = (name || '').trim()
  if (!m) return 'Other'
  for (const [re, vendor] of RULES) {
    if (re.test(m)) return vendor
  }
  if (m.includes('/')) {
    const head = m.split('/')[0]
    if (head) return head.charAt(0).toUpperCase() + head.slice(1)
  }
  return 'Other'
}

