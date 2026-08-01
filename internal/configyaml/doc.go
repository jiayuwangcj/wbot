// Package configyaml renders ~/.wbot/config.yaml (deployment config, see doc/PRIVACY.md)
// into a flat env mapping: nested keys become UPPER_SNAKE and ${VAR} expands from the environment.
package configyaml
