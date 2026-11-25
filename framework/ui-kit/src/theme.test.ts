import { createThemeSnapshot, validateBrandTokens } from './theme'

describe('theme', () => {
  it('resolves system mode and clones palette tokens', () => {
    const snapshot = createThemeSnapshot('system', 7, true)
    expect(snapshot.resolved).toBe('dark')
    expect(snapshot.revision).toBe(7)
    expect(validateBrandTokens(snapshot.tokens)).toBeNull()
  })

  it('rejects brand tokens without readable contrast', () => {
    const snapshot = createThemeSnapshot('light', 1)
    snapshot.tokens.text = snapshot.tokens.canvas
    expect(validateBrandTokens(snapshot.tokens)).toBeInstanceOf(Error)
  })
})
