const app = getApp()

let socketTask = null
let reconnectTimer = null
let reconnectAttempt = 0
const MAX_RECONNECT_DELAY = 30000
let listeners = {}

function connect() {
  if (socketTask) return

  const token = app.globalData.token
  if (!token) return

  socketTask = wx.connectSocket({
    url: app.globalData.wsURL + '?token=' + token,
    success() {
      console.log('[WS] connecting...')
    }
  })

  socketTask.onOpen(() => {
    console.log('[WS] connected')
    reconnectAttempt = 0
  })

  socketTask.onMessage((res) => {
    try {
      const msg = JSON.parse(res.data)
      const type = msg.type
      if (listeners[type]) {
        listeners[type].forEach(cb => cb(msg.data))
      }
    } catch (e) {
      console.error('[WS] parse error:', e)
    }
  })

  socketTask.onClose(() => {
    console.log('[WS] disconnected')
    socketTask = null
    scheduleReconnect()
  })

  socketTask.onError((err) => {
    console.error('[WS] error:', err)
    socketTask = null
    scheduleReconnect()
  })
}

function scheduleReconnect() {
  if (reconnectTimer) return
  reconnectAttempt++
  const delay = Math.min(1000 * Math.pow(2, reconnectAttempt - 1), MAX_RECONNECT_DELAY)
  console.log(`[WS] reconnecting in ${delay}ms (attempt ${reconnectAttempt})`)
  reconnectTimer = setTimeout(() => {
    reconnectTimer = null
    connect()
  }, delay)
}

function disconnect() {
  if (reconnectTimer) {
    clearTimeout(reconnectTimer)
    reconnectTimer = null
  }
  if (socketTask) {
    socketTask.close()
    socketTask = null
  }
}

function on(type, callback) {
  if (!listeners[type]) {
    listeners[type] = []
  }
  listeners[type].push(callback)
}

function off(type, callback) {
  if (!listeners[type]) return
  if (callback) {
    listeners[type] = listeners[type].filter(cb => cb !== callback)
  } else {
    delete listeners[type]
  }
}

module.exports = { connect, disconnect, on, off }
