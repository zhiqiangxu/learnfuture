const request = require('../../utils/request')
const wsManager = require('../../utils/ws')

Page({
  data: {
    price: '--', priceDir: '',
    side: 1, orderType: 1, leverage: 10,
    margin: '', limitPrice: '',
    takeProfit: '', stopLoss: '',
    available: '--',
    showTPSL: false, showCalcDetail: true, showCalcExpand: true,
    positionValue: '0', quantity: '0', fee: '0',
    liqPrice: '0', forceTpPrice: '0',
    feeRateDisplay: '0.04%',
    submitting: false,
    showTutorial: false, tutorialData: null,
  },

  onShow() {
    this.loadAccount()
    wsManager.connect()
    wsManager.on('ticker', this.onTicker.bind(this))
  },

  onHide() {
    wsManager.off('ticker', this.onTicker)
  },

  onTicker(data) {
    const old = parseFloat(this.data.price) || 0
    const cur = parseFloat(data.p)
    this.setData({
      price: data.p,
      priceDir: cur >= old ? 'color-up' : 'color-down'
    })
    this.recalculate()
  },

  loadAccount() {
    request.get('/api/v1/account/info').then(data => {
      this.setData({ available: data.available })
    }).catch(() => {
      getApp().ensureLogin().then(() => this.loadAccount())
    })
  },

  setSide(e) { this.setData({ side: parseInt(e.currentTarget.dataset.side) }); this.recalculate() },
  setOrderType(e) {
    const t = parseInt(e.currentTarget.dataset.type)
    this.setData({
      orderType: t,
      feeRateDisplay: t === 2 ? '0.02%' : '0.04%'
    })
    this.recalculate()
  },
  onLeverage(e) { this.setData({ leverage: e.detail.value }); this.recalculate() },
  setLeverage(e) { this.setData({ leverage: parseInt(e.currentTarget.dataset.lev) }); this.recalculate() },
  onMargin(e) { this.setData({ margin: e.detail.value }); this.recalculate() },
  onLimitPrice(e) { this.setData({ limitPrice: e.detail.value }); this.recalculate() },
  onTP(e) { this.setData({ takeProfit: e.detail.value }) },
  onSL(e) { this.setData({ stopLoss: e.detail.value }) },
  toggleTPSL() { this.setData({ showTPSL: !this.data.showTPSL }) },
  toggleCalcDetail() { this.setData({ showCalcExpand: !this.data.showCalcExpand }) },

  setMarginPct(e) {
    const pct = parseInt(e.currentTarget.dataset.pct) / 100
    const avail = parseFloat(this.data.available) || 0
    this.setData({ margin: (avail * pct).toFixed(2) })
    this.recalculate()
  },

  recalculate() {
    const margin = parseFloat(this.data.margin) || 0
    if (margin <= 0) return

    const lev = this.data.leverage
    const p = this.data.orderType === 2 && this.data.limitPrice
      ? parseFloat(this.data.limitPrice)
      : parseFloat(this.data.price)
    if (!p || p <= 0) return

    const posVal = margin * lev
    const qty = posVal / p
    const feeRate = this.data.orderType === 2 ? 0.0002 : 0.0004
    const fee = posVal * feeRate

    const side = this.data.side
    const maintRate = 0.005
    let liqPrice, ftpPrice
    if (side === 1) {
      liqPrice = p * (1 - 1/lev + maintRate)
      ftpPrice = p * (1 + 5/lev)
    } else {
      liqPrice = p * (1 + 1/lev - maintRate)
      ftpPrice = p * (1 - 5/lev)
    }

    this.setData({
      positionValue: posVal.toFixed(2),
      quantity: qty.toFixed(8),
      fee: fee.toFixed(4),
      liqPrice: liqPrice.toFixed(2),
      forceTpPrice: ftpPrice.toFixed(2),
    })
  },

  placeOrder() {
    if (this.data.submitting) return
    const margin = this.data.margin
    if (!margin || parseFloat(margin) <= 0) {
      wx.showToast({ title: '请输入保证金', icon: 'none' }); return
    }

    this.setData({ submitting: true })

    const params = {
      side: this.data.side,
      order_type: this.data.orderType,
      leverage: this.data.leverage,
      margin: margin,
    }
    if (this.data.orderType === 2) params.price = this.data.limitPrice
    if (this.data.takeProfit) params.take_profit = this.data.takeProfit
    if (this.data.stopLoss) params.stop_loss = this.data.stopLoss

    request.post('/api/v1/order/place', params).then(data => {
      wx.showToast({ title: data.status === 'filled' ? '开仓成功' : '挂单成功', icon: 'success' })
      this.setData({ margin: '', limitPrice: '', takeProfit: '', stopLoss: '' })
      this.loadAccount()
    }).catch(err => {
      wx.showToast({ title: err.message || '下单失败', icon: 'none' })
    }).finally(() => {
      this.setData({ submitting: false })
    })
  },

  onTutorial(tutorial) { this.setData({ showTutorial: true, tutorialData: tutorial }) },
  closeTutorial() { this.setData({ showTutorial: false }) },
})
