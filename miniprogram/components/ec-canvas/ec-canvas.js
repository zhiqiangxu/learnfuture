// ECharts Canvas 组件
// 实际使用时需要引入 echarts.min.js (从 echarts-for-weixin 获取)
// 参考: https://github.com/nicholasess/echarts-for-weixin

Component({
  properties: {
    canvasId: { type: String, value: 'ec-canvas' },
    ec: { type: Object }
  },

  lifetimes: {
    ready() {
      if (!this.data.ec) return
      this.initChart()
    }
  },

  methods: {
    initChart() {
      const query = this.createSelectorQuery()
      query.select('#' + this.data.canvasId)
        .fields({ node: true, size: true })
        .exec((res) => {
          if (!res[0]) return
          const canvas = res[0].node
          const ctx = canvas.getContext('2d')
          const dpr = wx.getWindowInfo().pixelRatio
          canvas.width = res[0].width * dpr
          canvas.height = res[0].height * dpr

          // 如果引入了 echarts，在这里初始化
          // const chart = echarts.init(canvas, null, { width: res[0].width, height: res[0].height, devicePixelRatio: dpr })
          // this.data.ec.onInit(chart)

          this.triggerEvent('bindinit', { canvas, ctx, width: res[0].width, height: res[0].height })
        })
    },

    touchStart(e) { this.triggerEvent('bindtouchstart', e) },
    touchMove(e) { this.triggerEvent('bindtouchmove', e) },
    touchEnd(e) { this.triggerEvent('bindtouchend', e) },
  }
})
