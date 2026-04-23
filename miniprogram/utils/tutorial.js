// 教学状态管理
const request = require('./request')

let completedTopics = {}
let loaded = false

function loadProgress() {
  if (loaded) return Promise.resolve()
  return request.get('/api/v1/tutorial/progress').then(data => {
    loaded = true
    if (data && data.topics) {
      data.topics.forEach(id => { completedTopics[id] = true })
    }
    return data
  })
}

function isCompleted(topicID) {
  return !!completedTopics[topicID]
}

function markComplete(topicID) {
  completedTopics[topicID] = true
  return request.post('/api/v1/tutorial/complete', { topic_id: topicID })
}

function getProgress() {
  return loadProgress()
}

function reset() {
  completedTopics = {}
  loaded = false
}

module.exports = { loadProgress, isCompleted, markComplete, getProgress, reset }
