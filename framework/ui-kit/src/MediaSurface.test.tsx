import { render } from '@testing-library/react'
import { allowMediaSrc, cancelMedia, MediaSurface } from './MediaSurface'

describe('MediaSurface', () => {
  it('accepts only bounded HTTPS sources', () => {
    expect(allowMediaSrc('https://example.com/demo.mp4')).toBe(true)
    expect(allowMediaSrc('http://example.com/insecure.mp4')).toBe(false)
    expect(allowMediaSrc('javascript:alert(1)')).toBe(false)
    expect(allowMediaSrc('data:video/mp4;base64,abc')).toBe(false)
    expect(allowMediaSrc('')).toBe(false)
  })

  it('uses lazy loading and disables controls in blocked states', () => {
    const { container, rerender } = render(<MediaSurface ariaLabel="演示视频" src="https://example.com/demo.mp4" />)
    const video = container.querySelector('video')!
    expect(video.preload).toBe('none')
    expect(video.controls).toBe(true)
    rerender(<MediaSurface ariaLabel="演示视频" src="https://example.com/demo.mp4" busy />)
    expect(container.querySelector('video')!.controls).toBe(false)
    rerender(<MediaSurface ariaLabel="演示音频" src="https://example.com/demo.mp3" kind="audio" disabled />)
    const audio = container.querySelector('audio')!
    expect(audio.controls).toBe(false)
    expect(audio.preload).toBe('none')
  })

  it('cancels playback and releases the media source', () => {
    const element = document.createElement('video')
    element.pause = vi.fn()
    element.load = vi.fn()
    element.src = 'https://example.com/demo.mp4'
    cancelMedia(element)
    expect(element.pause).toHaveBeenCalledOnce()
    expect(element.load).toHaveBeenCalledOnce()
    expect(element.hasAttribute('src')).toBe(false)
    expect(() => cancelMedia(null)).not.toThrow()
  })
})
