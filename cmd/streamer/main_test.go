package main

import (
	"path/filepath"
	"testing"
)

func TestResolveSource(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "Movies", "Example.mp4")
	resolvedInput, resolvedRoot, relative, err := resolveSource(input, root)
	if err != nil {
		t.Fatal(err)
	}
	if resolvedInput != input || resolvedRoot != root || relative != "Movies/Example.mp4" {
		t.Fatalf("resolveSource = %q, %q, %q", resolvedInput, resolvedRoot, relative)
	}
	_, defaultRoot, defaultRelative, err := resolveSource(input, "")
	if err != nil || defaultRoot != filepath.Dir(input) || defaultRelative != "Example.mp4" {
		t.Fatalf("default root = %q, %q, %v", defaultRoot, defaultRelative, err)
	}
	outside := filepath.Join(filepath.Dir(root), "outside.mp4")
	if _, _, _, err := resolveSource(outside, root); err == nil {
		t.Fatal("source outside library root was accepted")
	}
}
