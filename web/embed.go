package web

import "embed"

// Files 将网页资源编译进程序，分发无需Node.js或独立资源目录。
//
//go:embed index.html style.css app.js
var Files embed.FS
