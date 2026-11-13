package main

import (
	"context"

	"Pisar/internal/convert"
)

// The minimum version of pandoc is enforced here and nowhere else: every call
// into internal/convert and internal/preview that runs pandoc goes through one
// of these wrappers, which ask ready first. Spreading the check over the entry
// points of the services would let a new one forget it.

// pandocGuardedConverter refuses the conversions that run pandoc while ready
// says it may not be run. The ones that do not run it pass straight through.
type pandocGuardedConverter struct {
	inner wordConverter
	ready func(ctx context.Context) error
}

var _ wordConverter = pandocGuardedConverter{}

// DocxToMarkdown runs pandoc.
func (g pandocGuardedConverter) DocxToMarkdown(ctx context.Context, src, dir string) ([]convert.Loss, error) {
	if err := g.ready(ctx); err != nil {
		return nil, err
	}
	return g.inner.DocxToMarkdown(ctx, src, dir)
}

// MarkdownToDocx runs pandoc.
func (g pandocGuardedConverter) MarkdownToDocx(ctx context.Context, dir, reference, dst string) error {
	if err := g.ready(ctx); err != nil {
		return err
	}
	return g.inner.MarkdownToDocx(ctx, dir, reference, dst)
}

// DocToDocx runs textutil, not pandoc.
func (g pandocGuardedConverter) DocToDocx(ctx context.Context, src, dst string) error {
	return g.inner.DocToDocx(ctx, src, dst)
}

// MakeReference only reads and writes the .docx archive.
func (g pandocGuardedConverter) MakeReference(ctx context.Context, src, dst string) error {
	return g.inner.MakeReference(ctx, src, dst)
}

// pandocGuardedRenderer refuses to render while ready says pandoc may not be run.
type pandocGuardedRenderer struct {
	inner previewRenderer
	ready func(ctx context.Context) error
}

var _ previewRenderer = pandocGuardedRenderer{}

// Render runs pandoc.
func (g pandocGuardedRenderer) Render(ctx context.Context, markdown []byte, resourceDir string) ([]byte, error) {
	if err := g.ready(ctx); err != nil {
		return nil, err
	}
	return g.inner.Render(ctx, markdown, resourceDir)
}
