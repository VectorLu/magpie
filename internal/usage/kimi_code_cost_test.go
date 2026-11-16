package usage

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/catalog"
	"github.com/yetone/magpie/internal/provider"
	"github.com/yetone/magpie/internal/sessions"
	"github.com/yetone/magpie/internal/settings"
)

// A direct Kimi Code session names its model "kimi-code/k3.1-pro". The
// request ledger prices it at the Kimi API even with no provider saved, and
// a user's own price still wins.
func TestKimiCodeSessionCost(t *testing.T) {
	for _, tc := range []struct {
		name   string
		prices map[string]settings.ModelPrice
		saved  bool
		want   float64
	}{
		{name: "API price without a saved provider", want: 14.2},
		{name: "the user's price stands", prices: map[string]settings.ModelPrice{
			"*/k3.1-pro": {Input: new(float64(0.1)), Output: new(float64(0.2)), CacheRead: new(float64(0.02)), CacheWrite: new(float64(0.04))},
		}, saved: true, want: 0.36},
		{name: "the full session model's price stands", prices: map[string]settings.ModelPrice{
			"session-unknown/kimi-code/k3.1-pro": {Input: new(float64(0.1)), Output: new(float64(0.2)), CacheRead: new(float64(0.02)), CacheWrite: new(float64(0.04))},
		}, saved: true, want: 0.36},
		{name: "the unknown provider's price stands", prices: map[string]settings.ModelPrice{
			"session-unknown/*": {Input: new(float64(0.1)), Output: new(float64(0.2)), CacheRead: new(float64(0.02)), CacheWrite: new(float64(0.04))},
		}, saved: true, want: 0.36},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
			t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
			b, err := os.ReadFile(filepath.Join("..", "provider", "testdata", "kimi_code_catalog.json"))
			if err != nil {
				t.Fatal(err)
			}
			os.MkdirAll(filepath.Dir(catalog.CachePath()), 0o755)
			if err := os.WriteFile(catalog.CachePath(), b, 0o644); err != nil {
				t.Fatal(err)
			}
			catalog.Reset()
			t.Cleanup(catalog.Reset)
			if tc.saved {
				p, err := provider.FromPreset("kimi-code")
				if err != nil {
					t.Fatal(err)
				}
				p.Key = "sk-test"
				if err := provider.Save(p); err != nil {
					t.Fatal(err)
				}
			}
			if tc.prices != nil {
				if err := settings.Save(settings.Settings{ModelPrices: tc.prices}); err != nil {
					t.Fatal(err)
				}
			}

			now := time.Now()
			old := LogCalls
			LogCalls = func(time.Time) []sessions.Call {
				return []sessions.Call{{Time: now, Agent: "kimi", Session: "kimi-session", Model: "kimi-code/k3.1-pro",
					Tokens: sessions.Tokens{Input: 1_000_000, Output: 1_000_000, CacheRead: 1_000_000, CacheWrite: 1_000_000}}}
			}
			t.Cleanup(func() { LogCalls = old })

			rows, sum, _ := Ledger(Month, Filter{})
			if len(rows) != 1 {
				t.Fatalf("rows %d", len(rows))
			}
			r := rows[0]
			if r.Source != "log" || r.Provider != UnknownProvider || r.Model != "kimi-code/k3.1-pro" || !r.Priced || math.Abs(r.Cost-tc.want) > 1e-9 {
				t.Errorf("session call: priced=%v cost=%v, want %v; row %+v", r.Priced, r.Cost, tc.want, r.Record)
			}
			if sum.Unpriced != 0 || math.Abs(sum.Cost-tc.want) > 1e-9 {
				t.Errorf("summary cost %v unpriced %d, want %v and 0", sum.Cost, sum.Unpriced, tc.want)
			}
		})
	}
}

// #1370 (maicent): the Usage page counted a Kimi Code membership's k3 at
// ≈¥0.000, its plan's models.dev catalog listing every model at $0. A k3
// call is now counted at Kimi's API price for kimi-k3, a k3.1-pro one at
// kimi-k3.1-pro's, kimi-for-coding (K2.8 Preview, which the API doesn't
// sell) is unpriced rather than free, and a Moonshot pay-as-you-go call is
// what it was.
func TestKimiCodeCost(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	b, err := os.ReadFile(filepath.Join("..", "provider", "testdata", "kimi_code_catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Dir(catalog.CachePath()), 0o755)
	if err := os.WriteFile(catalog.CachePath(), b, 0o644); err != nil {
		t.Fatal(err)
	}
	catalog.Reset()
	t.Cleanup(catalog.Reset)
	for _, id := range []string{"kimi-code-cn", "moonshot"} {
		p, err := provider.FromPreset(id)
		if err != nil {
			t.Fatal(err)
		}
		p.Key = "sk-test"
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
	}
	// the reporter's k3 row: 240K in, 78.4K out, 10.0M cached
	in, out, cached := 240_000, 78_400, 10_000_000
	k3 := (float64(in)*3 + float64(out)*15 + float64(cached)*0.3) / 1e6
	k31 := (float64(in)*2 + float64(out)*10 + float64(cached)*0.2) / 1e6
	now := time.Now()
	calls := []struct {
		prov, model string
		priced      bool
		want        float64
	}{
		{"kimi-code-cn", "k3", true, k3},
		{"kimi-code-cn", "k3.1-pro", true, k31},
		{"kimi-code-cn", "kimi-for-coding", false, 0},
		{"moonshot", "kimi-k3", true, k3},
	}
	for i, c := range calls {
		Append(Record{Time: now.Add(-time.Duration(len(calls)-i) * time.Minute), Agent: "claude-code", Provider: c.prov, Model: c.model,
			Input: in, Output: out, CacheRead: cached, Status: 200})
	}
	rows, sum, _ := Ledger(Month, Filter{})
	if len(rows) != len(calls) {
		t.Fatalf("rows %d", len(rows))
	}
	for i, c := range calls {
		r := rows[len(calls)-1-i]
		if r.Model != c.model || r.Priced != c.priced || math.Abs(r.Cost-c.want) > 1e-9 {
			t.Errorf("%s/%s: priced=%v cost=%v, want %v %v", c.prov, c.model, r.Priced, r.Cost, c.priced, c.want)
		}
	}
	s := Summarize(Month)
	if s.Unpriced != 1 || math.Abs(s.Cost-(2*k3+k31)) > 1e-9 || math.Abs(sum.Cost-(2*k3+k31)) > 1e-9 {
		t.Fatalf("summary cost %v unpriced %d, ledger %v; want %v and 1", s.Cost, s.Unpriced, sum.Cost, 2*k3+k31)
	}
}
