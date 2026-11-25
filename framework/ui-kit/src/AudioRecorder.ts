import { speakVoiceOverState } from './voiceover'

/** W116-G674：对照旧版 AudioRecorder 再对照。旧版无独立 ui-kit AudioRecorder 入口。诚实不画。不含产品语义。不 VoiceOver。不改业务 RPC。 */
export const audioRecorderClassName = 'mzgui-audio-recorder'

export type AudioRecorderState = 'idle' | 'requesting' | 'recording' | 'paused' | 'stopping'

export type AudioRecorderSnapshot = {
  state: AudioRecorderState
  durationMS: number
  mimeType: string
}

export type AudioRecording = {
  blob: Blob
  durationMS: number
  mimeType: string
}

export type AudioRecorderDependencies = {
  getUserMedia?: (constraints: MediaStreamConstraints) => Promise<MediaStream>
  createMediaRecorder?: (stream: MediaStream, options: MediaRecorderOptions) => MediaRecorder
  createAudioContext?: () => AudioContext
  isTypeSupported?: (mimeType: string) => boolean
  now?: () => number
}

const mimeCandidates = [
  'audio/webm;codecs=opus',
  'audio/mp4;codecs=mp4a.40.2',
  'audio/ogg;codecs=opus',
]

export class BrowserAudioRecorder {
  private state: AudioRecorderState = 'idle'
  private stream?: MediaStream
  private recorder?: MediaRecorder
  private audioContext?: AudioContext
  private source?: MediaStreamAudioSourceNode
  private analyser?: AnalyserNode
  private samples?: Uint8Array<ArrayBuffer>
  private chunks: Blob[] = []
  private startedAt = 0
  private pausedAt = 0
  private pausedDuration = 0
  private mimeType = ''
  private readonly getUserMedia: (constraints: MediaStreamConstraints) => Promise<MediaStream>
  private readonly createMediaRecorder: (stream: MediaStream, options: MediaRecorderOptions) => MediaRecorder
  private readonly createAudioContext: () => AudioContext
  private readonly isTypeSupported: (mimeType: string) => boolean
  private readonly now: () => number

  constructor(dependencies: AudioRecorderDependencies = {}) {
    this.getUserMedia = dependencies.getUserMedia ?? ((constraints) => {
      if (!navigator.mediaDevices?.getUserMedia) return Promise.reject(new Error('当前浏览器引擎不支持麦克风录音'))
      return navigator.mediaDevices.getUserMedia(constraints)
    })
    this.createMediaRecorder = dependencies.createMediaRecorder ?? ((stream, options) => new MediaRecorder(stream, options))
    this.createAudioContext = dependencies.createAudioContext ?? (() => {
      const WebkitAudioContext = (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext
      const Context = window.AudioContext ?? WebkitAudioContext
      if (!Context) throw new Error('当前浏览器引擎不支持音频电平采集')
      return new Context()
    })
    this.isTypeSupported = dependencies.isTypeSupported ?? ((mimeType) => {
      if (typeof MediaRecorder === 'undefined') return Boolean(dependencies.createMediaRecorder && mimeType === mimeCandidates[0])
      return MediaRecorder.isTypeSupported(mimeType)
    })
    this.now = dependencies.now ?? (() => performance.now())
  }

  snapshot(): AudioRecorderSnapshot {
    return { state: this.state, durationMS: this.durationMS(), mimeType: this.mimeType }
  }

  speak(): string {
    return speakVoiceOverState('status', '录音', this.state)
  }

  async start(onChunk?: (chunk: Blob, mimeType: string) => void): Promise<AudioRecorderSnapshot> {
    if (this.state !== 'idle') throw new Error('录音已经开始')
    this.state = 'requesting'
    try {
      this.mimeType = mimeCandidates.find((value) => this.isTypeSupported(value)) ?? ''
      if (!this.mimeType) throw new Error('当前浏览器引擎不支持可保存的录音格式')
      this.stream = await this.getUserMedia({ audio: { channelCount: 1, echoCancellation: true, noiseSuppression: true }, video: false })
      this.audioContext = this.createAudioContext()
      this.source = this.audioContext.createMediaStreamSource(this.stream)
      this.analyser = this.audioContext.createAnalyser()
      this.analyser.fftSize = 1024
      this.analyser.smoothingTimeConstant = 0.72
      this.samples = new Uint8Array(this.analyser.frequencyBinCount)
      this.source.connect(this.analyser)
      this.recorder = this.createMediaRecorder(this.stream, { mimeType: this.mimeType, audioBitsPerSecond: 128000 })
      this.chunks = []
      this.recorder.ondataavailable = (event) => {
        if (!event.data.size) return
        if (onChunk) onChunk(event.data, event.data.type || this.mimeType)
        else this.chunks.push(event.data)
      }
      this.startedAt = this.now()
      this.pausedAt = 0
      this.pausedDuration = 0
      this.recorder.start(500)
      this.state = 'recording'
      return this.snapshot()
    } catch (error) {
      await this.cleanup()
      this.state = 'idle'
      if (error instanceof DOMException && error.name === 'NotAllowedError') throw new Error('麦克风权限未开启，请在系统设置中允许当前应用访问麦克风')
      throw error
    }
  }

  pause(): AudioRecorderSnapshot {
    if (this.state !== 'recording' || !this.recorder) throw new Error('当前没有可暂停的录音')
    this.recorder.pause()
    this.pausedAt = this.now()
    this.state = 'paused'
    return this.snapshot()
  }

  async resume(): Promise<AudioRecorderSnapshot> {
    if (this.state !== 'paused' || !this.recorder) throw new Error('当前没有可继续的录音')
    if (this.audioContext?.state === 'suspended') await this.audioContext.resume()
    this.pausedDuration += Math.max(0, this.now() - this.pausedAt)
    this.pausedAt = 0
    this.recorder.resume()
    this.state = 'recording'
    return this.snapshot()
  }

  level(): number {
    if (this.state !== 'recording' || !this.analyser || !this.samples) return 0
    this.analyser.getByteTimeDomainData(this.samples)
    let sum = 0
    for (const value of this.samples) {
      const normalized = (value - 128) / 128
      sum += normalized * normalized
    }
    return Math.min(1, Math.sqrt(sum / this.samples.length) * 3.2)
  }

  async stop(): Promise<AudioRecording> {
    const result = await this.stopCapture(false)
    if (!result) throw new Error('录音文件为空')
    return result
  }

  /** 分片已交给调用方；等待最后一次 dataavailable 后释放采集设备。 */
  async stopStreaming(): Promise<void> {
    await this.stopCapture(true)
  }

  private async stopCapture(streaming: boolean): Promise<AudioRecording | undefined> {
    if ((this.state !== 'recording' && this.state !== 'paused') || !this.recorder) throw new Error('当前没有正在录制的音频')
    const recorder = this.recorder
    const durationMS = this.durationMS()
    this.state = 'stopping'
    return new Promise<AudioRecording | undefined>((resolve, reject) => {
      recorder.onerror = () => {
        void this.cleanup().finally(() => {
          this.state = 'idle'
          reject(new Error('录音设备停止失败'))
        })
      }
      recorder.onstop = () => {
        const mimeType = recorder.mimeType || this.mimeType
        const blob = streaming ? undefined : new Blob(this.chunks, { type: mimeType })
        this.chunks = []
        void this.cleanup().then(() => {
          this.state = 'idle'
          if (!blob) resolve(undefined)
          else if (blob.size < 1) reject(new Error('录音文件为空'))
          else resolve({ blob, durationMS: Math.max(1, Math.round(durationMS)), mimeType })
        }, reject)
      }
      recorder.stop()
    })
  }

  async cancel(): Promise<void> {
    if (this.recorder && this.recorder.state !== 'inactive') this.recorder.stop()
    await this.cleanup()
    this.state = 'idle'
  }

  private durationMS(): number {
    if (!this.startedAt) return 0
    const end = this.state === 'paused' && this.pausedAt ? this.pausedAt : this.now()
    return Math.max(0, end - this.startedAt - this.pausedDuration)
  }

  private async cleanup(): Promise<void> {
    this.source?.disconnect()
    this.analyser?.disconnect()
    this.stream?.getTracks().forEach((track) => track.stop())
    if (this.audioContext && this.audioContext.state !== 'closed') await this.audioContext.close()
    this.stream = undefined
    this.recorder = undefined
    this.audioContext = undefined
    this.source = undefined
    this.analyser = undefined
    this.samples = undefined
    this.startedAt = 0
    this.pausedAt = 0
    this.pausedDuration = 0
  }
}
