package stages

import (
	"fmt"
	"strings"
)

type PlanOptions struct {
	FromID  string
	OnlyIDs []string
	SkipIDs []string
}

func ResolvePlan(catalog []Stage, options PlanOptions) ([]string, error) {
	ids := IDs(catalog)
	index := make(map[string]int, len(ids))
	for i, id := range ids {
		index[id] = i
	}

	plan := make([]string, 0, len(ids))

	if len(options.OnlyIDs) > 0 {
		onlySet := make(map[string]struct{}, len(options.OnlyIDs))
		for _, id := range options.OnlyIDs {
			normalized := strings.TrimSpace(id)
			if normalized == "" {
				continue
			}
			if _, ok := index[normalized]; !ok {
				return nil, fmt.Errorf("unknown stage id in --only: %s", normalized)
			}
			onlySet[normalized] = struct{}{}
		}
		for _, id := range ids {
			if _, ok := onlySet[id]; ok {
				plan = append(plan, id)
			}
		}
	} else {
		plan = append(plan, ids...)
	}

	if from := strings.TrimSpace(options.FromID); from != "" {
		fromIndex, ok := index[from]
		if !ok {
			return nil, fmt.Errorf("unknown stage id in --from: %s", from)
		}

		filtered := make([]string, 0, len(plan))
		for _, id := range plan {
			if index[id] >= fromIndex {
				filtered = append(filtered, id)
			}
		}
		plan = filtered
	}

	if len(options.SkipIDs) > 0 {
		skipSet := make(map[string]struct{}, len(options.SkipIDs))
		for _, id := range options.SkipIDs {
			normalized := strings.TrimSpace(id)
			if normalized == "" {
				continue
			}
			if _, ok := index[normalized]; !ok {
				return nil, fmt.Errorf("unknown stage id in --skip: %s", normalized)
			}
			skipSet[normalized] = struct{}{}
		}

		filtered := make([]string, 0, len(plan))
		for _, id := range plan {
			if _, skip := skipSet[id]; skip {
				continue
			}
			filtered = append(filtered, id)
		}
		plan = filtered
	}

	if len(plan) == 0 {
		return nil, fmt.Errorf("resolved stage plan is empty")
	}

	return plan, nil
}
