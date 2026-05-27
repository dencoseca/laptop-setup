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
	stageByID := make(map[StageID]Stage, len(catalog))
	for i, id := range ids {
		index[id] = i
		stageByID[id] = catalog[i]
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
		if err := validateCriticalStagesIncluded(catalog, index, onlySet, strings.TrimSpace(options.FromID)); err != nil {
			return nil, err
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
			if stageByID[stageID].Critical {
				return nil, fmt.Errorf("stage %s cannot be skipped", normalized)
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

func validateCriticalStagesIncluded(catalog []Stage, index map[StageID]int, included map[StageID]struct{}, from string) error {
	fromIndex := 0
	if from != "" {
		fromID := StageID(from)
		resolvedIndex, ok := index[fromID]
		if !ok {
			return fmt.Errorf("unknown stage id in --from: %s", from)
		}
		fromIndex = resolvedIndex
	}
	for _, stage := range catalog {
		if !stage.Critical {
			continue
		}
		if index[stage.ID] < fromIndex {
			continue
		}
		if _, ok := included[stage.ID]; !ok {
			return fmt.Errorf("critical stage %s must be included", stage.ID)
		}
	}
	return nil
}
