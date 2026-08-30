package analytics

import _ "embed"

//go:embed dashboard.html
var dashboardHTML []byte

//go:embed dashboard.js
var dashboardJS []byte

func DashboardHTML() []byte { return append([]byte(nil), dashboardHTML...) }
func DashboardJS() []byte   { return append([]byte(nil), dashboardJS...) }
