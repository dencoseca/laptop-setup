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

func ResolvePlan(catalog []Stage, options PlanOptions) ([]StageID, error) {
	ids := IDs(catalog)
	index := make(map[StageID]int, len(ids))
	for i, id := range ids {
		index[id] = i
	}

	plan := make([]StageID, 0, len(ids))

	if len(options.OnlyIDs) > 0 {
		onlySet := make(map[StageID]struct{}, len(options.OnlyIDs))
		for _, id := range options.OnlyIDs {
			normalized := strings.TrimSpace(id)
			if normalized == "" {
				continue
			}
			stageID := StageID(normalized)
			if _, ok := index[stageID]; !ok {
				return nil, fmt.Errorf("unknown stage id in --only: %s", normalized)
			}
			onlySet[stageID] = struct{}{}
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
		fromID := StageID(from)
		fromIndex, ok := index[fromID]
		if !ok {
			return nil, fmt.Errorf("unknown stage id in --from: %s", from)
		}

		filtered := make([]StageID, 0, len(plan))
		for _, id := range plan {
			if index[id] >= fromIndex {
				filtered = append(filtered, id)
			}
		}
		plan = filtered
	}

	if len(options.SkipIDs) > 0 {
		skipSet := make(map[StageID]struct{}, len(options.SkipIDs))
		for _, id := range options.SkipIDs {
			normalized := strings.TrimSpace(id)
			if normalized == "" {
				continue
			}
			stageID := StageID(normalized)
			if _, ok := index[stageID]; !ok {
				return nil, fmt.Errorf("unknown stage id in --skip: %s", normalized)
			}
			skipSet[stageID] = struct{}{}
		}

		filtered := make([]StageID, 0, len(plan))
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
