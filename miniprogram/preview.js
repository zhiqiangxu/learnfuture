const ci = require('miniprogram-ci')
const path = require('path')

async function main() {
  const project = new ci.Project({
    appid: 'wx23b8ee6dd9fe941d',
    type: 'miniProgram',
    projectPath: path.resolve(__dirname),
    privateKeyPath: path.resolve(__dirname, 'private.key'),
    ignores: ['node_modules/**/*', 'preview.js', 'upload.js', 'private.key']
  })

  const qrcodePath = path.resolve(__dirname, 'preview-qrcode.jpg')

  console.log('Generating preview QR code...')
  await ci.preview({
    project,
    desc: 'LearnFuture 合约模拟学习平台预览',
    setting: {
      es6: true,
      es7: true,
      minify: true,
      autoPrefixWXSS: true
    },
    qrcodeFormat: 'image',
    qrcodeOutputDest: qrcodePath,
    pagePath: 'pages/market/market'
  })

  console.log('QR code saved to:', qrcodePath)
  console.log('Scan with WeChat to preview!')
}

main().catch(err => {
  console.error('Preview failed:', err.message || err)
  process.exit(1)
})
