<script lang="ts" module>
  export type CodeBlockLanguage = 'awg-conf' | 'plain';
</script>

<script lang="ts">
  interface Props {
    text: string;
    language?: CodeBlockLanguage;
    lineNumbers?: boolean;
    class?: string;
  }

  let { text, language = 'plain', lineNumbers = false, class: cls = '' }: Props = $props();

  let lines = $derived(text.split('\n'));

  type Token = { kind: 'comment' | 'section' | 'label' | 'value-num' | 'value-text' | 'eq' | 'plain'; text: string };

  function tokenize(line: string, lang: CodeBlockLanguage): Token[] {
    if (lang !== 'awg-conf') return [{ kind: 'plain', text: line }];
    const trimmed = line.trim();
    if (trimmed.startsWith('#') || trimmed.startsWith(';')) {
      return [{ kind: 'comment', text: line }];
    }
    if (trimmed.startsWith('[') && trimmed.endsWith(']')) {
      return [{ kind: 'section', text: line }];
    }
    const eq = line.indexOf('=');
    if (eq < 0) return [{ kind: 'plain', text: line }];
    const labelPart = line.slice(0, eq);
    const valuePart = line.slice(eq + 1);
    const valueTrimmed = valuePart.trim();
    const isNumericish = /^[\d.,\s\-/+:a-zA-Z]+$/.test(valueTrimmed) && /^[\d]/.test(valueTrimmed);
    return [
      { kind: 'label', text: labelPart },
      { kind: 'eq', text: '=' },
      { kind: isNumericish ? 'value-num' : 'value-text', text: valuePart },
    ];
  }

  let parsed = $derived(lines.map((l) => tokenize(l, language)));
</script>

<pre class="codeblock {cls}" data-line-numbers={lineNumbers ? 'true' : 'false'}>
{#each parsed as tokens, i (i)}
<span class="line">
  {#if lineNumbers}<span class="ln">{i + 1}</span>{/if}
  <span class="content">{#each tokens as t}<span class={`tok ${t.kind}`}>{t.text}</span>{/each}</span>
</span>
{/each}
</pre>

<style>
  .codeblock {
    background: var(--color-bg-secondary);
    color: var(--color-text-secondary);
    font: 400 12px/1.7 var(--font-mono);
    padding: 14px 18px;
    margin: 0;
    overflow-x: auto;
    white-space: pre;
  }
  .line { display: block; }
  .ln {
    display: inline-block;
    width: 24px;
    text-align: right;
    color: var(--color-text-muted-soft);
    margin-right: 14px;
    user-select: none;
  }
  .tok.comment    { color: var(--color-text-muted); }
  .tok.section    { color: var(--color-info); }
  .tok.label      { color: var(--color-text-secondary); }
  .tok.value-num  { color: var(--color-yellow); }
  .tok.value-text { color: var(--color-text-primary); }
  .tok.eq         { color: var(--color-text-muted); }
  .tok.plain      { color: var(--color-text-secondary); }
</style>
