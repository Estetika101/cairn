package seo

import "github.com/Estetika101/cairn/internal/model"

// All returns every SEO check (v0.4 §5d), page-scoped and site-scoped, each
// registered under its own stable ID so config can enable/disable individually.
func All() []model.Check {
	return []model.Check{
		Title(),
		MetaDescription(),
		MetaDescriptionLength(),
		Canonical(),
		SingleH1(),
		HeadingOrder(),
		ImgAltCoverage(),
		MetaRobots(),
		Viewport(),
		OpenGraph(),
		TwitterCard(),
		HreflangReciprocity(),
		SitemapValid(),
		SitemapCoverage(),
		DuplicateTitle(),
		DuplicateMetaDescription(),
	}
}
