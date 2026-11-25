/** W116-G658：对照旧版 SegmentedControl / Tabs i18n 再对照。旧版无 i18n catalog，字号小中大硬编码。不含产品语义。不 VoiceOver。不改业务 RPC。 */
export function formatMessage(
  template: string,
  values?: Record<string, string | number>,
  fallback?: string,
): string {
  const source = template.trim()
  if (source === '' || !source.includes('{')) {
    if (fallback && (source === '' || source.includes('.'))) {
      return fallback
    }
    return fallback && source === 'missing.key' ? fallback : source
  }
  return source.replace(/\{([a-zA-Z0-9_]+)\}/g, (_, name: string) => {
    if (!values || values[name] === undefined) {
      return fallback ?? `{${name}}`
    }
    return String(values[name])
  })
}
