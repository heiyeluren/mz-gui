let currentTaskID = ''
let pollTimer

async function call(name, ...parameters) {
  return window.wails.Call.ByName(`main.DesktopService.${name}`, ...parameters)
}

function showFeedback(message, isError = false) {
  const feedback = document.querySelector('#feedback')
  feedback.textContent = message
  feedback.classList.toggle('error', isError)
}

function render(task) {
  currentTaskID = task.id
  document.querySelector('#task-id').textContent = task.id || '尚未启动'
  document.querySelector('#task-state').textContent = task.state || 'idle'
  document.querySelector('#task-result').textContent = task.result_summary_json || '—'
  document.querySelector('#cancel').disabled = !task.id || ['succeeded', 'failed', 'cancelled'].includes(task.state)
}

async function refresh() {
  if (!currentTaskID) {
    showFeedback('请先启动一个后台任务')
    return
  }
  try {
    render(await call('GetTask', currentTaskID))
    showFeedback('任务状态已从所选 TaskBackend 读取')
  } catch (error) {
    showFeedback(`读取失败：${error}`, true)
  }
}

async function start() {
  const button = document.querySelector('#start')
  button.disabled = true
  try {
    render(await call('StartTask', JSON.stringify({ source: 'desktop-example' })))
    showFeedback('任务已提交；关闭桌面不会终止 agentd 中的任务')
    window.clearInterval(pollTimer)
    pollTimer = window.setInterval(async () => {
      const task = await call('GetTask', currentTaskID)
      render(task)
      if (['succeeded', 'failed', 'cancelled'].includes(task.state)) window.clearInterval(pollTimer)
    }, 100)
  } catch (error) {
    showFeedback(`启动失败：${error}`, true)
  } finally {
    button.disabled = false
  }
}

const panelTitles = { tasks: '后台任务', architecture: '进程架构', migration: '迁移方式' }
document.querySelectorAll('.nav-item').forEach((button) => {
  button.addEventListener('click', () => {
    const target = button.dataset.panel
    document.querySelectorAll('.nav-item').forEach((item) => item.classList.toggle('active', item === button))
    document.querySelectorAll('.panel').forEach((panel) => { panel.hidden = panel.id !== `panel-${target}` })
    document.querySelector('#page-title').textContent = panelTitles[target]
  })
})
document.querySelector('#start').addEventListener('click', start)
document.querySelector('#refresh').addEventListener('click', refresh)
document.querySelector('#cancel').addEventListener('click', async () => {
  try {
    render(await call('CancelTask', currentTaskID))
    showFeedback('任务已取消')
  } catch (error) {
    showFeedback(`取消失败：${error}`, true)
  }
})
