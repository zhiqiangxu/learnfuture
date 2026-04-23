const request = require('../../utils/request')
const wsManager = require('../../utils/ws')

Page({
  data: {
    positions: [],
    showTutorial: false, tutorialData: null,
  },

  onShow() {
    this.loadPositions()
    wsManager.connect()
    wsManager.on('position_pnl', this.onPnlUpdate.bind(this))
    wsManager.on('liquidated', this.onLiquidated.bind(this))
    wsManager.on('force_tp', this.onForceTP.bind(this))
  },

  onHide() {
    wsManager.off('position_pnl')
    wsManager.off('liquidated')
    wsManager.off('force_tp')
  },

  onPullDownRefresh() {
    this.loadPositions().finally(() => wx.stopPullDownRefresh())
  },

  loadPositions() {
    return request.get('/api/v1/position/list').then(data => {
      this.setData({ positions: data.positions || [] })
    })
  },

  onPnlUpdate(data) {
    const positions = this.data.positions.map(p => {
      if (p.id === data.id) {
        return { ...p, unrealized_pnl: data.upnl, margin_ratio: data.margin_ratio, roi: data.roi, adl_indicator: data.adl_indicator }
      }
      return p
    })
    this.setData({ positions })
  },

  onLiquidated(data) {
    wx.showModal({
      title: '仓位被强平',
      content: `仓位在 ${data.liq_price} 被强制平仓，损失 ${data.loss} USDT`,
      showCancel: false
    })
    this.loadPositions()
  },

  onForceTP(data) {
    wx.showModal({
      title: '仓位被强盈',
      content: `仓位在 ${data.price} 被强制止盈，盈利 ${data.profit} USDT`,
      showCancel: false
    })
    this.loadPositions()
  },

  closePosition(e) {
    const id = e.currentTarget.dataset.id
    wx.showModal({
      title: '确认平仓',
      content: '确定要平仓吗？',
      success: (res) => {
        if (res.confirm) {
          request.post('/api/v1/position/close', { position_id: id }).then(data => {
            wx.showToast({ title: `平仓成功，净盈亏 ${data.net_pnl} USDT`, icon: 'none', duration: 3000 })
            this.loadPositions()
          }).catch(err => {
            wx.showToast({ title: err.message, icon: 'none' })
          })
        }
      }
    })
  },

  editTPSL(e) {
    const id = e.currentTarget.dataset.id
    wx.showModal({
      title: '设置止盈止损',
      content: '功能开发中',
      showCancel: false
    })
  },

  marginRatioColor(ratio) {
    const r = parseFloat(ratio)
    if (r > 0.1) return 'green'
    if (r > 0.02) return 'yellow'
    return 'red'
  },

  onTutorial(tutorial) { this.setData({ showTutorial: true, tutorialData: tutorial }) },
  closeTutorial() { this.setData({ showTutorial: false }) },
})
