package handoff

import (
	"fmt"
	"slices"

	"github.com/LunetteOrg/ai-profiles/internal/config"
	"github.com/LunetteOrg/ai-profiles/internal/launch"
	"github.com/LunetteOrg/ai-profiles/internal/usage"
)

// Combo is a profile and an agent.
type Combo struct {
	Profile string
	Agent   string
}

// Allowed lists the combinations a folder admits, the folder's profile
// first: its agents, then those of the other profiles it admits (handoff_to,
// or the profiles of a folder in [[folders]]). Agents without an adapter are
// left out.
func Allowed(cfg *config.Config, m config.Match) []Combo {
	profiles := m.Admitted()
	var out []Combo
	for _, name := range profiles {
		for _, a := range cfg.Profiles[name].Agents {
			if _, ok := launch.Adapters[a]; ok {
				c := Combo{name, a}
				if !slices.Contains(out, c) {
					out = append(out, c)
				}
			}
		}
	}
	return out
}

// Best picks the combination with the most margin: 100 minus its most used
// window. Combinations without data come after those with data; ties keep the
// order of combos.
func Best(combos []Combo, rows []usage.Row) (Combo, string) {
	best, bestMargin := combos[0], -1.0
	why := "no usage data: using the first allowed combination"
	for _, c := range combos {
		for _, r := range rows {
			if r.Profile != c.Profile || r.Agent != c.Agent || r.Err != nil || len(r.Windows) == 0 {
				continue
			}
			used := 0.0
			for _, w := range r.Windows {
				used = max(used, w.Used)
			}
			if margin := 100 - used; margin > bestMargin {
				best, bestMargin = c, margin
				why = fmt.Sprintf("%.0f%% headroom on the most used window", margin)
			}
		}
	}
	return best, why
}
