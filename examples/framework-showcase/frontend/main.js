async function call(name, ...parameters) {
  return window.wails.Call.ByName(`main.ShowcaseService.${name}`, ...parameters)
}

function showFeedback(message, isError = false) {
  const feedback = document.querySelector('#feedback')
  feedback.textContent = message
  feedback.classList.toggle('error', isError)
}

function render(snapshot) {
  document.querySelector('#lifecycle').textContent = snapshot.lifecycle_ready ? 'Ready' : 'Stopped'
  document.querySelector('#health').textContent = snapshot.health.status
  document.querySelector('#credential').textContent = snapshot.credential_set ? 'Available' : 'Unavailable'
  document.querySelector('#events').textContent = String(snapshot.event_count)
  const version = snapshot.framework_version
  const prerelease = version.prerelease ? `-${version.prerelease}` : ''
  document.querySelector('#framework-version').textContent = `v${version.major}.${version.minor}.${version.patch}${prerelease}`
}

async function load() {
  const refresh = document.querySelector('#refresh')
  refresh.disabled = true
  try {
    render(await call('GetSnapshot'))
    showFeedback('框架状态已更新')
  } catch {
    showFeedback('框架状态读取失败，请检查 Go 服务日志', true)
  } finally {
    refresh.disabled = false
  }
}

const panelTitles = {
  overview: '框架概览',
  capabilities: '公共能力',
  quickstart: '快速开始',
}

document.querySelectorAll('.nav-item').forEach((button) => {
  button.addEventListener('click', () => {
    const target = button.dataset.panel
    document.querySelectorAll('.nav-item').forEach((item) => item.classList.toggle('active', item === button))
    document.querySelectorAll('.panel').forEach((panel) => {
      panel.hidden = panel.id !== `panel-${target}`
    })
    document.querySelector('#page-title').textContent = panelTitles[target]
  })
})

document.querySelector('#refresh').addEventListener('click', load)
document.querySelector('#emit').addEventListener('click', async () => {
  const button = document.querySelector('#emit')
  button.disabled = true
  try {
    await call('EmitSample')
    await new Promise((resolve) => window.setTimeout(resolve, 20))
    render(await call('GetSnapshot'))
    showFeedback('事件已通过 Wails → Go → EventBus 完整投递')
  } catch {
    showFeedback('事件发送失败，请检查 Go 服务日志', true)
  } finally {
    button.disabled = false
  }
})

load()
