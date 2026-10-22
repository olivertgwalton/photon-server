// The web app holds no Go. This module fences it off from photon-server's, so a Go file an npm
// package ships under node_modules is no package of the server's for go build, lint or test.
module github.com/olivertgwalton/photon-server/web
