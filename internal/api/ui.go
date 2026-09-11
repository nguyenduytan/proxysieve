package api

import (
	"bytes"
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

//go:embed ui/* ui/assets/*
var uiAssets embed.FS

func dashboardHandler() http.Handler {
	root, err := fs.Sub(uiAssets, "ui")
	if err != nil {
		panic(err)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if clean == "" || clean == "." {
			clean = "index.html"
		}
		if _, err := fs.Stat(root, clean); err != nil {
			if strings.HasPrefix(clean, "assets/") || path.Ext(clean) != "" {
				http.NotFound(w, r)
				return
			}
			clean = "index.html"
		}
		data, err := fs.ReadFile(root, clean)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		http.ServeContent(w, r, clean, time.Time{}, bytes.NewReader(data))
	})
}
