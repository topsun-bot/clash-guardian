package discovery

import (
	"testing"

	"github.com/clash-guardian/clash-guardian/internal/clash"
)

func TestRecommendGroupsInRuleModeAvoidsGlobal(t *testing.T) {
	proxies := map[string]clash.Proxy{
		"GLOBAL": {Type: "Selector", Now: "node-a", All: []string{"node-a", "node-b", "node-c"}},
		"🚀代理线路":  {Type: "Selector", Now: "node-a", All: []string{"node-a", "node-b"}},
		"🤖AI服务":  {Type: "Selector", Now: "node-b", All: []string{"node-a", "node-b"}},
	}
	got := RecommendGroups(proxies, "rule")
	if len(got) == 0 || got[0].Name != "🚀代理线路" {
		t.Fatalf("rule 模式应推荐实际代理线路，得到 %#v", got)
	}
}

func TestRecommendGroupsInGlobalModePrefersGlobal(t *testing.T) {
	proxies := map[string]clash.Proxy{
		"GLOBAL": {Type: "Selector", Now: "node-a", All: []string{"node-a", "node-b"}},
		"🚀代理线路":  {Type: "Selector", Now: "node-a", All: []string{"node-a", "node-b", "node-c"}},
	}
	got := RecommendGroups(proxies, "global")
	if len(got) == 0 || got[0].Name != "GLOBAL" {
		t.Fatalf("global 模式应推荐 GLOBAL，得到 %#v", got)
	}
}
