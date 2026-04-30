const request = require('../../utils/request')
const wsManager = require('../../utils/ws')

Page({
  data: {
    price: '--',
    change: '0.00',
    high: '--',
    low: '--',
    markPrice: '--',
    indexPrice: '--',
    basis: '0.000000',
    priceDirection: '',
    fundingRate: '0.0000',
    nextSettle: '--:--',
    insuranceFund: '--',
    intervals: ['1s', '5s', '15s', '1m', '5m', '15m', '1h', '4h', '1d'],
    currentInterval: '1m',
    klineLoaded: false,
    showTutorial: false,
    tutorialData: null,
  },

  onLoad() {
    console.log('[Market] onLoad, baseURL:', getApp().globalData.baseURL)
    // 直接测试请求
    wx.request({
      url: 'https://learnfuture.cc/api/v1/market/price',
      success: (res) => console.log('[Market] direct request success:', JSON.stringify(res.data)),
      fail: (err) => console.error('[Market] direct request fail:', JSON.stringify(err))
    })
    this.loadPrice()
    this.loadMarkPrice()
    this.loadFundingRate()
    this.loadInsuranceFund()
  },

  onShow() {
    wsManager.connect()
    wsManager.on('ticker', this.onTicker.bind(this))
  },

  onHide() {
    wsManager.off('ticker', this.onTicker)
  },

  onTicker(data) {
    const oldPrice = parseFloat(this.data.price) || 0
    const newPrice = parseFloat(data.p)
    this.setData({
      price: data.p,
      change: data.c,
      high: data.h,
      low: data.l,
      priceDirection: newPrice >= oldPrice ? 'color-up' : 'color-down'
    })
  },

  loadPrice() {
    request.get('/api/v1/market/price').then(data => {
      this.setData({
        price: data.price,
        change: data.change,
        high: data.high,
        low: data.low,
      })
    }).catch(err => console.error('[Market] loadPrice failed:', err))
  },

  loadMarkPrice() {
    request.get('/api/v1/market/mark-price').then(data => {
      this.setData({
        markPrice: parseFloat(data.mark_price).toFixed(2),
        indexPrice: parseFloat(data.index_price).toFixed(2),
        basis: (parseFloat(data.basis) * 100).toFixed(4),
      })
    }).catch(err => console.error('[Market] loadMarkPrice failed:', err))
  },

  loadFundingRate() {
    request.get('/api/v1/market/funding-rate').then(data => {
      const rate = (parseFloat(data.rate) * 100).toFixed(4)
      const next = new Date(data.next_settle * 1000)
      const hh = String(next.getHours()).padStart(2, '0')
      const mm = String(next.getMinutes()).padStart(2, '0')
      this.setData({
        fundingRate: rate,
        nextSettle: hh + ':' + mm + ' UTC'
      })
    }).catch(err => console.error('[Market] loadFundingRate failed:', err))
  },

  loadInsuranceFund() {
    request.get('/api/v1/market/insurance-fund').then(data => {
      this.setData({ insuranceFund: data.balance })
    }).catch(err => console.error('[Market] loadInsuranceFund failed:', err))
  },

  switchInterval(e) {
    const interval = e.currentTarget.dataset.interval
    this.setData({ currentInterval: interval, klineLoaded: false })
    this.loadKlines(interval)
  },

  loadKlines(interval) {
    request.get('/api/v1/market/klines', { interval: interval, limit: 100 }).then(data => {
      this.setData({ klineLoaded: true })
      // TODO: render klines with ECharts
    })
  },

  goTrade(e) {
    const side = e.currentTarget.dataset.side
    wx.switchTab({ url: '/pages/trade/trade' })
  },

  onTutorial(tutorial) {
    this.setData({ showTutorial: true, tutorialData: tutorial })
  },

  closeTutorial() {
    this.setData({ showTutorial: false })
  }
})
