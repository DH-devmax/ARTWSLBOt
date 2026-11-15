<template>
  <textarea
    ref="element"
    class="assistant-textarea"
    :value="modelValue"
    rows="1"
    @input="emit('update:modelValue', ($event.target as HTMLTextAreaElement).value)"
    @keydown="emit('keydown', $event)"
  />
</template>
<script setup lang="ts">
  import { nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
  const props = defineProps<{ modelValue: string }>()
  const emit = defineEmits<{
    'update:modelValue': [value: string]
    keydown: [event: KeyboardEvent]
  }>()
  const element = ref<HTMLTextAreaElement>()
  let observer: ResizeObserver | undefined,
    width = 0
  function resize() {
    const input = element.value
    if (!input?.isConnected) return
    const style = getComputedStyle(input)
    const line = Number.parseFloat(style.lineHeight)
    if (!Number.isFinite(line)) return
    input.style.height = '0px'
    const height = Math.min(Math.max(input.scrollHeight, line), line * 5)
    input.style.height = `${height}px`
    input.style.overflowY = input.scrollHeight > height ? 'auto' : 'hidden'
  }
  watch(
    () => props.modelValue,
    () => {
      void nextTick(resize)
    }
  )
  onMounted(() => {
    resize()
    observer = new ResizeObserver((entries) => {
      const next = entries[0]?.contentRect.width
      if (next !== undefined && next !== width) {
        width = next
        resize()
      }
    })
    if (element.value) observer.observe(element.value)
  })
  onUnmounted(() => observer?.disconnect())
  defineExpose({ focus: () => element.value?.focus() })
</script>
<style scoped>
  .assistant-textarea {
    display: block;
    box-sizing: border-box;
    width: 100%;
    min-height: 22px;
    max-height: 110px;
    padding: 0;
    border: 0;
    border-radius: 0;
    background: transparent;
    color: var(--ui-ink);
    font: inherit;
    font-size: 14px;
    line-height: 22px;
    letter-spacing: 0;
    resize: none;
    outline: none;
    box-shadow: none;
    overflow-y: hidden;
    scrollbar-gutter: stable;
  }
  .assistant-textarea::placeholder {
    color: var(--ui-muted);
  }
  .assistant-textarea:disabled {
    cursor: not-allowed;
    color: var(--ui-muted);
  }
</style>
