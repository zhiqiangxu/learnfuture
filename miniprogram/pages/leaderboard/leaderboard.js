const request = require('../../utils/request')

Page({
  data: { tab: 'pnl', list: [], myRank: null },
  onLoad() { this.loadData() },
  switchTab(e) {
    this.setData({ tab: e.currentTarget.dataset.tab })
    this.loadData()
  },
  loadData() {
    const url = this.data.tab === 'pnl' ? '/api/v1/leaderboard/pnl' : '/api/v1/leaderboard/roi'
    request.get(url).then(data => {
      this.setData({ list: data.list || [], myRank: data.my || null })
    })
  },
})
