<template>
  <div class="assistant-markdown" v-html="rendered" />
</template>

<script setup lang="ts">
  import { computed } from 'vue'
  import MarkdownIt from 'markdown-it'

  const props = defineProps<{ content: string }>()
  const markdown = new MarkdownIt({ html: false, linkify: false, breaks: true }).disable('image')
  markdown.renderer.rules.table_open = () =>
    '<div class="assistant-table-scroll" role="region" aria-label="回答表格" tabindex="0"><table>'
  markdown.renderer.rules.table_close = () => '</table></div>'
  markdown.renderer.rules.link_open = (tokens, index, options, _env, self) => {
    tokens[index].attrSet('target', '_blank')
    tokens[index].attrSet('rel', 'noopener noreferrer')
    return self.renderToken(tokens, index, options)
  }
  const rendered = computed(() => markdown.render(props.content))
</script>

<style scoped>
  .assistant-markdown {
    min-width: 0;
    max-width: 100%;
    color: var(--ui-text);
    font-size: 14px;
    line-height: 1.75;
    overflow-wrap: anywhere;
    white-space: normal;
  }
  .assistant-markdown :deep(> :first-child) {
    margin-top: 0;
  }
  .assistant-markdown :deep(> :last-child) {
    margin-bottom: 0;
  }
  .assistant-markdown :deep(h1),
  .assistant-markdown :deep(h2),
  .assistant-markdown :deep(h3),
  .assistant-markdown :deep(h4),
  .assistant-markdown :deep(h5),
  .assistant-markdown :deep(h6) {
    margin: 20px 0 10px;
    color: var(--ui-ink);
    font-weight: 600;
    line-height: 1.5;
    letter-spacing: 0;
  }
  .assistant-markdown :deep(h1) {
    font-size: 20px;
  }
  .assistant-markdown :deep(h2) {
    font-size: 18px;
  }
  .assistant-markdown :deep(h3) {
    font-size: 16px;
  }
  .assistant-markdown :deep(h4),
  .assistant-markdown :deep(h5),
  .assistant-markdown :deep(h6) {
    font-size: 14px;
  }
  .assistant-markdown :deep(p),
  .assistant-markdown :deep(ul),
  .assistant-markdown :deep(ol),
  .assistant-markdown :deep(blockquote),
  .assistant-markdown :deep(pre) {
    margin: 0 0 12px;
  }
  .assistant-markdown :deep(ul),
  .assistant-markdown :deep(ol) {
    padding-left: 1.6em;
  }
  .assistant-markdown :deep(li + li) {
    margin-top: 4px;
  }
  .assistant-markdown :deep(li > p) {
    margin-bottom: 4px;
  }
  .assistant-markdown :deep(blockquote) {
    padding: 4px 12px;
    border-left: 3px solid var(--ui-line);
    color: var(--ui-muted);
  }
  .assistant-markdown :deep(blockquote > :last-child) {
    margin-bottom: 0;
  }
  .assistant-markdown :deep(code) {
    padding: 2px 5px;
    border-radius: 4px;
    background: var(--ui-soft);
    font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
    font-size: 0.9em;
  }
  .assistant-markdown :deep(pre) {
    max-width: 100%;
    padding: 12px;
    border: 1px solid var(--ui-line);
    border-radius: 6px;
    background: var(--ui-soft);
    overflow-x: auto;
    white-space: pre;
    overscroll-behavior-x: contain;
  }
  .assistant-markdown :deep(pre code) {
    padding: 0;
    background: transparent;
  }
  .assistant-markdown :deep(a) {
    color: var(--el-color-primary);
    text-decoration: underline;
  }
  .assistant-markdown :deep(hr) {
    margin: 16px 0;
    border: 0;
    border-top: 1px solid var(--ui-line);
  }
  .assistant-markdown :deep(.assistant-table-scroll) {
    max-width: 100%;
    margin: 12px 0;
    overflow-x: auto;
    overscroll-behavior-x: contain;
    border: 1px solid var(--ui-line);
    border-radius: 6px;
  }
  .assistant-markdown :deep(table) {
    width: max-content;
    min-width: 100%;
    border-collapse: collapse;
    font-size: 13px;
  }
  .assistant-markdown :deep(th),
  .assistant-markdown :deep(td) {
    min-width: 110px;
    max-width: 360px;
    padding: 9px 12px;
    border-bottom: 1px solid var(--ui-line);
    text-align: left;
    vertical-align: top;
  }
  .assistant-markdown :deep(th) {
    background: var(--ui-soft);
    font-weight: 600;
  }
  .assistant-markdown :deep(tr:last-child td) {
    border-bottom: 0;
  }
</style>
