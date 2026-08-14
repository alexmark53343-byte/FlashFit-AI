//go:build windows && !embedmodel

package main

// Development builds carry no model. The app then looks for a runtime and
// weights the user has supplied in the models directory, and simply runs
// without the advisor if there are none.

func advisorHasEmbeddedModel() bool { return false }

func ensureEmbeddedAssets(progress func(string)) (server string, model string, err error) {
	return findAdvisorAssets()
}
