// Package checks is the registry of built-in checks. Built-ins register through
// the same model.Check interface a WASM plugin is adapted to — there is no
// privileged built-in path in the registry (v0.4 §2c).
package checks

import (
	"github.com/Estetika101/cairn/internal/checks/links"
	"github.com/Estetika101/cairn/internal/checks/security"
	"github.com/Estetika101/cairn/internal/checks/seo"
	"github.com/Estetika101/cairn/internal/model"
)

// Builtins returns every compiled-in check. The caller filters by config
// (module or check-ID enable) before running them.
func Builtins() []model.Check {
	all := []model.Check{
		security.New(),
		links.New(),
	}
	all = append(all, seo.All()...)
	return all
}
