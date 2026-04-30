function request(url, method, data) {
  const app = getApp()
  return new Promise((resolve, reject) => {
    wx.request({
      url: app.globalData.baseURL + url,
      method: method,
      data: data,
      header: {
        'Content-Type': 'application/json',
        'Authorization': app.globalData.token ? 'Bearer ' + app.globalData.token : ''
      },
      success(res) {
        if (res.statusCode === 401) {
          // Token 过期，尝试刷新
          refreshAndRetry(url, method, data).then(resolve).catch(reject)
          return
        }
        const body = res.data
        if (body.code === 0) {
          // 处理教学卡片
          if (body.tutorial) {
            showTutorialCard(body.tutorial)
          }
          resolve(body.data)
        } else {
          reject(new Error(body.message || '请求失败'))
        }
      },
      fail(err) {
        reject(err)
      }
    })
  })
}

function refreshAndRetry(url, method, data) {
  const app = getApp()
  return new Promise((resolve, reject) => {
    wx.request({
      url: app.globalData.baseURL + '/api/v1/auth/refresh',
      method: 'POST',
      data: { refresh_token: app.globalData.refreshToken },
      header: { 'Content-Type': 'application/json' },
      success(res) {
        if (res.data.code === 0) {
          app.globalData.token = res.data.data.token
          app.globalData.refreshToken = res.data.data.refresh_token
          wx.setStorageSync('token', res.data.data.token)
          wx.setStorageSync('refreshToken', res.data.data.refresh_token)
          // Retry original request
          request(url, method, data).then(resolve).catch(reject)
        } else {
          // Refresh failed, re-login
          app.login().then(() => {
            request(url, method, data).then(resolve).catch(reject)
          }).catch(reject)
        }
      },
      fail: reject
    })
  })
}

function showTutorialCard(tutorial) {
  const pages = getCurrentPages()
  const currentPage = pages[pages.length - 1]
  if (currentPage && currentPage.onTutorial) {
    currentPage.onTutorial(tutorial)
  }
}

module.exports = {
  get: (url, data) => request(url, 'GET', data),
  post: (url, data) => request(url, 'POST', data),
}
