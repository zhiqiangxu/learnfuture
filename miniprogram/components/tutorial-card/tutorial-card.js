const tutorial = require('../../utils/tutorial')

Component({
  properties: {
    show: { type: Boolean, value: false },
    tutorial: { type: Object, value: null }
  },

  methods: {
    onClose() {
      this.setData({ show: false })
      this.triggerEvent('close')
    },

    onGotIt() {
      const t = this.data.tutorial
      if (t && t.id) {
        tutorial.markComplete(t.id)
      }
      this.onClose()
    }
  }
})
