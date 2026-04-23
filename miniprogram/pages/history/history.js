const request = require('../../utils/request')

Page({
  data: {
    tab: 'orders', orders: [], trades: [], fundings: [],
    pnlSummary: {}, loading: false, page: 1,
  },
  onShow() { this.loadData() },
  switchTab(e) {
    this.setData({ tab: e.currentTarget.dataset.tab, page: 1 })
    this.loadData()
  },
  loadData() {
    this.setData({ loading: true })
    const tab = this.data.tab
    if (tab === 'orders') {
      request.get('/api/v1/history/orders', { page: this.data.page }).then(d => this.setData({ orders: d.orders || [] })).finally(() => this.setData({ loading: false }))
    } else if (tab === 'trades') {
      request.get('/api/v1/history/trades', { page: this.data.page }).then(d => this.setData({ trades: d.trades || [] })).finally(() => this.setData({ loading: false }))
    } else if (tab === 'funding') {
      request.get('/api/v1/history/funding', { page: this.data.page }).then(d => this.setData({ fundings: d.fundings || [] })).finally(() => this.setData({ loading: false }))
    } else if (tab === 'pnl') {
      request.get('/api/v1/history/pnl').then(d => this.setData({ pnlSummary: d })).finally(() => this.setData({ loading: false }))
    }
  },
})
