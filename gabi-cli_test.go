package main

import (
	"strings"
	"testing"

	"github.com/app-sre/gabi/pkg/models"
	routev1 "github.com/openshift/api/route/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func testRoute(name, host string) routev1.Route {
	return routev1.Route{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       routev1.RouteSpec{Host: host},
	}
}

func TestSelectGabiRoute(t *testing.T) {
	prod := testRoute("gabi-myapp", "gabi-myapp-prod.example.com")
	restore := testRoute("gabi-myapp-restore", "gabi-myapp-prod-restore.example.com")
	both := []routev1.Route{prod, restore}

	t.Run("single route auto-selects", func(t *testing.T) {
		got, err := selectGabiRoute([]routev1.Route{prod}, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Name != prod.Name {
			t.Fatalf("got %s, want %s", got.Name, prod.Name)
		}
	})

	t.Run("multiple routes without -r errors", func(t *testing.T) {
		_, err := selectGabiRoute(both, "")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "use -r") {
			t.Fatalf("expected -r hint, got: %v", err)
		}
	})

	t.Run("exact name match", func(t *testing.T) {
		got, err := selectGabiRoute(both, "gabi-myapp-restore")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Name != restore.Name {
			t.Fatalf("got %s, want %s", got.Name, restore.Name)
		}
	})

	t.Run("substring does not match", func(t *testing.T) {
		_, err := selectGabiRoute(both, "restore")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "no gabi route named") {
			t.Fatalf("expected no-match error, got: %v", err)
		}
	})

	t.Run("no match lists available routes", func(t *testing.T) {
		_, err := selectGabiRoute(both, "missing")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "gabi-myapp-restore") {
			t.Fatalf("expected available routes in error, got: %v", err)
		}
	})
}

func TestFormatResultRaw(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		result [][]string
		want   string
	}{
		{name: "no result rows"},
		{
			name:   "header only",
			result: [][]string{{"QUERY PLAN"}},
		},
		{
			name:   "one cell JSON result",
			result: [][]string{{"QUERY PLAN"}, {"[{\"Plan\": {\"Node Type\": \"Result\"}}]"}},
			want:   "[{\"Plan\": {\"Node Type\": \"Result\"}}]\n",
		},
		{
			name:   "multi-line cell is preserved",
			result: [][]string{{"QUERY PLAN"}, {"[\n  {\"Plan\": {}\n]"}},
			want:   "[\n  {\"Plan\": {}\n]\n",
		},
		{
			name:   "multiple rows and columns",
			result: [][]string{{"schema", "name"}, {"public", "users"}, {"audit", "events"}},
			want:   "public\tusers\naudit\tevents\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var out strings.Builder
			formatResult(models.QueryResponse{Result: tt.result}, &out, false, "raw")

			if got := out.String(); got != tt.want {
				t.Errorf("formatResult(..., raw) = %q, want %q", got, tt.want)
			}
		})
	}
}
