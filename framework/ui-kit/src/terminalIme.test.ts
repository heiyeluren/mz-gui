import { bindTerminalIme, isMacWebKit } from './terminalIme'

describe('terminalIme', () => {
  it('commits final composition text without sending the preview', async () => {
    const textarea = document.createElement('textarea')
    document.body.append(textarea)
    const ime = bindTerminalIme(textarea, true)
    textarea.dispatchEvent(new CompositionEvent('compositionstart'))
    textarea.dispatchEvent(new CompositionEvent('compositionupdate', { data: 'ni hao' }))
    textarea.dispatchEvent(new CompositionEvent('compositionend', { data: '你好' }))
    expect(ime.filter('ni hao')).toBe('你好')
    await new Promise(resolve => setTimeout(resolve, 0))
    expect(ime.filter('你好')).toBe('你好')
    ime.dispose()
    textarea.remove()
  })

  it('detects Mac WebKit without treating Chrome or other platforms as WebKit', () => {
    expect(isMacWebKit('Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15')).toBe(true)
    expect(isMacWebKit('Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 Chrome/120.0.0.0')).toBe(false)
    expect(isMacWebKit('Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36')).toBe(false)
  })
})
