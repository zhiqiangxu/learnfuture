const envConfig = require('./config')

App({
  globalData: {
    token: '',
    refreshToken: '',
    userID: 0,
    baseURL: envConfig.baseURL,
    wsURL: envConfig.wsURL,
  },

  onLaunch() {
    // 从本地缓存读取 token
    const token = wx.getStorageSync('token')
    const refreshToken = wx.getStorageSync('refreshToken')
    const userID = wx.getStorageSync('userID')
    if (token) {
      this.globalData.token = token
      this.globalData.refreshToken = refreshToken
      this.globalData.userID = userID
    }
  },

  // 微信登录
  login() {
    return new Promise((resolve, reject) => {
      wx.login({
        success: (res) => {
          if (res.code) {
            const request = require('./utils/request')
            request.post('/api/v1/auth/wx-login', { code: res.code })
              .then(data => {
                this.globalData.token = data.token
                this.globalData.refreshToken = data.refresh_token
                this.globalData.userID = data.user_id
                wx.setStorageSync('token', data.token)
                wx.setStorageSync('refreshToken', data.refresh_token)
                wx.setStorageSync('userID', data.user_id)
                resolve(data)
              })
              .catch(reject)
          } else {
            reject(new Error('微信登录失败'))
          }
        },
        fail: reject
      })
    })
  },

  // 确保已登录
  ensureLogin() {
    if (this.globalData.token) {
      return Promise.resolve()
    }
    return this.login()
  }
})
