import { ref } from 'vue'

/** 复制并短暂显示“copied”状态，避免只有 Toast 反馈。 */
export function useCopy() {
  const copied = ref('')
  async function copy(text: string, key: string): Promise<boolean> {
    try {
      await navigator.clipboard?.writeText(text)
      copied.value = key
      setTimeout(() => {
        if (copied.value === key) copied.value = ''
      }, 1200)
      return true
    } catch {
      return false
    }
  }
  return { copied, copy }
}
