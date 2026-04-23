// 环境配置 — 修改这里切换环境，不要提交真实值到 repo
const config = {
  // 开发环境
  dev: {
    baseURL: 'http://localhost:8888',
    wsURL: 'ws://localhost:8888/ws',
  },
  // 生产环境
  prod: {
    baseURL: 'https://learnfuture.cc',
    wsURL: 'wss://learnfuture.cc/ws',
  }
}

// 切换环境: 'dev' 或 'prod'
const env = 'dev'

module.exports = config[env]
