package main

import "embed"

// dist is the built SPA, written by "task web:build" (vite build, with
// outDir pointed at this directory). embed resolves relative to this
// file, which is why the directory sits under cmd/ rather than internal/;
// see AGENTS.md.
//
//go:embed dist
var distFS embed.FS
