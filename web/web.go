package web

import "embed"

// Content embeds all templates and static assets for FamilyBoard.
//
//go:embed templates/* static/*
var Content embed.FS
