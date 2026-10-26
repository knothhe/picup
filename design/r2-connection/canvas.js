window.H2_DESIGN_CANVAS = {
  "title": "Picup · R2 已连接卡片",
  "boards": [
    {
      "file": "connected-en-dark.html",
      "title": "已连接 · 英文深色",
      "x": 0,
      "y": 110,
      "w": 1040,
      "h": 760
    },
    {
      "file": "connected-en-narrow.html",
      "title": "已连接 · 760 英文深色",
      "x": 1120,
      "y": 110,
      "w": 760,
      "h": 620
    },
    {
      "file": "connected-zh-light.html",
      "title": "已连接 · 中文浅色",
      "x": 2240,
      "y": 110,
      "w": 1040,
      "h": 760
    },
    {
      "file": "disconnect-confirm-en.html",
      "title": "断开确认 · 英文深色",
      "x": 0,
      "y": 1010,
      "w": 1040,
      "h": 760
    },
    {
      "file": "disconnect-confirm-zh.html",
      "title": "断开确认 · 760 中文浅色",
      "x": 1120,
      "y": 1010,
      "w": 760,
      "h": 620
    },
    {
      "file": "disconnect-working-en.html",
      "title": "断开中 · 760 英文深色",
      "x": 2240,
      "y": 1010,
      "w": 760,
      "h": 620
    },
    {
      "file": "nonrenewable-en.html",
      "title": "不可静默续期 · 英文深色",
      "x": 0,
      "y": 1910,
      "w": 1040,
      "h": 760
    },
    {
      "file": "nonrenewable-zh.html",
      "title": "不可静默续期 · 760 中文浅色",
      "x": 1120,
      "y": 1910,
      "w": 760,
      "h": 620
    },
    {
      "file": "pending-cleanup-en.html",
      "title": "清理待完成 · 英文深色",
      "x": 2240,
      "y": 1910,
      "w": 1040,
      "h": 760
    },
    {
      "file": "metadata-loading-en.html",
      "title": "读取连接状态 · 760 英文深色",
      "x": 0,
      "y": 2810,
      "w": 760,
      "h": 620
    }
  ],
  "notes": [
    {
      "text": "正常卡片：16px内边距，32px header。Endpoint使用72px label + 12px间距 + 可省略value，完整地址保留tooltip。到期只显示实际日期；不承诺自动续期永远有效。",
      "x": 0,
      "y": 0,
      "w": 1040
    },
    {
      "text": "正常动作「连接其他存储桶…」→ 浏览器授权、选账号/桶后保存新profile，当前连接保留。断开确认集中在footer；处理中不提供未实现的取消能力。",
      "x": 1120,
      "y": 0,
      "w": 1040
    },
    {
      "text": "窄窗沿用760×620。非renewable提示重新连接并保存新配置；pending/working明确处理对象与保留图片，不把“尚待清理”说成已撤销。所有数据为本地虚构例子，按钮不发起真实操作。",
      "x": 2240,
      "y": 0,
      "w": 1040
    }
  ]
};
