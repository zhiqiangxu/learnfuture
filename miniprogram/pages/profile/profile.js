const request = require('../../utils/request')

Page({
  data: {
    balance: '--', available: '--', frozen: '--', totalPnl: '--',
    progress: { completed: 0, total: 13 },
  },
  onShow() {
    getApp().ensureLogin().then(() => this.loadInfo())
  },
  loadInfo() {
    request.get('/api/v1/account/info').then(data => {
      this.setData({
        balance: data.balance, available: data.available,
        frozen: data.frozen, totalPnl: data.total_pnl,
        progress: data.progress || { completed: 0, total: 13 },
      })
    })
  },
  resetAccount() {
    wx.showModal({
      title: '重置账户',
      content: '将清空所有历史数据，余额重置为 10,000 USDT，确定吗？',
      success: (res) => {
        if (res.confirm) {
          request.post('/api/v1/account/reset').then(data => {
            wx.showToast({ title: '重置成功', icon: 'success' })
            this.loadInfo()
          }).catch(err => wx.showToast({ title: err.message, icon: 'none' }))
        }
      }
    })
  },
  goLeaderboard() {
    wx.navigateTo({ url: '/pages/leaderboard/leaderboard' })
  },
})
