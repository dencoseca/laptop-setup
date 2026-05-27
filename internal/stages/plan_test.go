package stages

import "testing"

func TestResolvePlan(t *testing.T) {
	catalog := []Stage{
		{ID: "a"},
		{ID: "b"},
		{ID: "c"},
		{ID: "d"},
	}

	tests := []struct {
		name    string
		options PlanOptions
		want    []string
		wantErr bool
	}{
		{
			name: "default order",
			want: []string{"a", "b", "c", "d"},
		},
		{
			name: "only preserves catalog order",
			options: PlanOptions{
				OnlyIDs: []string{"d", "b"},
			},
			want: []string{"b", "d"},
		},
		{
			name: "from cuts earlier stages",
			options: PlanOptions{
				FromID: "c",
			},
			want: []string{"c", "d"},
		},
		{
			name: "skip removes stage",
			options: PlanOptions{
				SkipIDs: []string{"b"},
			},
			want: []string{"a", "c", "d"},
		},
		{
			name: "only plus skip",
			options: PlanOptions{
				OnlyIDs: []string{"a", "b"},
				SkipIDs: []string{"a"},
			},
			want: []string{"b"},
		},
		{
			name: "unknown only stage",
			options: PlanOptions{
				OnlyIDs: []string{"z"},
			},
			wantErr: true,
		},
		{
			name: "unknown from stage",
			options: PlanOptions{
				FromID: "z",
			},
			wantErr: true,
		},
		{
			name: "unknown skip stage",
			options: PlanOptions{
				SkipIDs: []string{"z"},
			},
			wantErr: true,
		},
		{
			name: "empty final plan",
			options: PlanOptions{
				OnlyIDs: []string{"a"},
				SkipIDs: []string{"a"},
			},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ResolvePlan(catalog, test.options)
			if test.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil and plan=%v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(test.want) {
				t.Fatalf("plan length mismatch: got=%v want=%v", got, test.want)
			}
			for i := range got {
				if got[i].String() != test.want[i] {
					t.Fatalf("plan mismatch at %d: got=%v want=%v", i, got, test.want)
				}
			}
		})
	}
}

func TestResolvePlanRejectsCriticalStageSkip(t *testing.T) {
	catalog := []Stage{
		{ID: "a"},
		{ID: "b", Critical: true},
		{ID: "c"},
	}

	_, err := ResolvePlan(catalog, PlanOptions{SkipIDs: []string{"b"}})
	if err == nil {
		t.Fatal("expected critical stage skip to fail")
	}
}

func TestResolvePlanRejectsOnlyThatOmitsCriticalStage(t *testing.T) {
	catalog := []Stage{
		{ID: "xcode_clt", Critical: true},
		{ID: "homebrew_install", Critical: true},
		{ID: "brew_bundle"},
	}

	_, err := ResolvePlan(catalog, PlanOptions{OnlyIDs: []string{"brew_bundle"}})
	if err == nil {
		t.Fatal("expected --only without critical stages to fail")
	}

	plan, err := ResolvePlan(catalog, PlanOptions{
		FromID:  "brew_bundle",
		OnlyIDs: []string{"brew_bundle"},
	})
	if err != nil {
		t.Fatalf("expected --from after critical stages to allow only brew_bundle: %v", err)
	}
	if len(plan) != 1 || plan[0] != "brew_bundle" {
		t.Fatalf("unexpected plan: %v", plan)
	}
}
